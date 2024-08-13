/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package logging

import (
	"io"
	"log/slog"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// LoggerBuilder contains the data and logic needed to create a logger. Don't create instances of
// this directly, use the NewLogger function instead.
type LoggerBuilder struct {
	writer io.Writer
	out    io.Writer
	err    io.Writer
	level  string
	file   string
	fields map[string]any
	redact bool
}

// NewLogger creates a builder that can then be used to configure and create a logger.
func NewLogger() *LoggerBuilder {
	return &LoggerBuilder{
		redact: true,
	}
}

// SetWriter sets the writer that the logger will write to. This is optional, and if not specified
// the the logger will write to the standard output stream of the process.
func (b *LoggerBuilder) SetWriter(value io.Writer) *LoggerBuilder {
	b.writer = value
	return b
}

// SetOut sets the standard output stream. This is optional and will only be used then the log file
// is 'stdout'.
func (b *LoggerBuilder) SetOut(value io.Writer) *LoggerBuilder {
	b.out = value
	return b
}

// SetErr sets the standard error output stream. This is optional and will only be used when the log
// file is 'stderr'.
func (b *LoggerBuilder) SetErr(value io.Writer) *LoggerBuilder {
	b.err = value
	return b
}

// AddField adds a field that will be added to all the log messages. The following field values have
// special meanings:
//
// - %p: Is replaced by the process identifier.
//
// Any other field value is added without change.
func (b *LoggerBuilder) AddField(name string, value any) *LoggerBuilder {
	if b.fields == nil {
		b.fields = map[string]any{}
	}
	b.fields[name] = value
	return b
}

// AddFields adds a set of fields that will be added to all the log messages. See the AddField
// method for the meanings of values.
func (b *LoggerBuilder) AddFields(values map[string]any) *LoggerBuilder {
	if b.fields == nil {
		b.fields = maps.Clone(values)
	} else {
		maps.Copy(b.fields, values)
	}
	return b
}

// SetFields sets the fields tht will be added to all the log messages. See the AddField method for
// the meanings of values. Note that this replaces any previously configured fields. If you want to
// preserve them use the AddFields method.
func (b *LoggerBuilder) SetFields(values map[string]any) *LoggerBuilder {
	b.fields = maps.Clone(values)
	return b
}

// SetLevel sets the log level.
func (b *LoggerBuilder) SetLevel(value string) *LoggerBuilder {
	b.level = value
	return b
}

// SetFile sets the file that the logger will write to. This is optional, and if not specified
// the the logger will write to the standard output stream of the process.
func (b *LoggerBuilder) SetFile(value string) *LoggerBuilder {
	b.file = value
	return b
}

// Set redact sets the flag that indicates if security sensitive data should be removed from the
// log. These fields are indicated by adding an exlamation mark in front of the field name. For
// example, to write a message with a `public` field that isn't sensitive and another `private`
// field that is:
//
//	logger.Info(
//		"SSH keys",
//		"public", publicKey,
//		"!public", privateKey,
//	)
//
// When redacting is enabled the value of the sensitive field will be replaced be `***`, so in the
// example above the resulting message will be like this:
//
//	{
//		"msg": "SSHKeys",
//		"public": "ssh-rsa AAA...",
//		"private": "***"
//	}
//
// The exclamation mark will be always removed from the field name.
func (b *LoggerBuilder) SetRedact(value bool) *LoggerBuilder {
	b.redact = value
	return b
}

// SetFlags sets the command line flags that should be used to configure the logger. This is
// optional.
func (b *LoggerBuilder) SetFlags(flags *pflag.FlagSet) *LoggerBuilder {
	if flags != nil {
		if flags.Changed(levelFlagName) {
			value, err := flags.GetString(levelFlagName)
			if err == nil {
				b.SetLevel(value)
			}
		}
		if flags.Changed(fileFlagName) {
			value, err := flags.GetString(fileFlagName)
			if err == nil {
				b.SetFile(value)
			}
		}
		if flags.Changed(fieldFlagName) {
			values, err := flags.GetStringArray(fieldFlagName)
			if err == nil {
				fields := b.parseFieldItems(values)
				b.AddFields(fields)
			}
		}
		if flags.Changed(fieldsFlagName) {
			values, err := flags.GetStringSlice(fieldsFlagName)
			if err == nil {
				fields := b.parseFieldItems(values)
				b.AddFields(fields)
			}
		}
		if flags.Changed(redactFlagName) {
			value, err := flags.GetBool(redactFlagName)
			if err == nil {
				b.SetRedact(value)
			}
		}
	}
	return b
}

func (b *LoggerBuilder) parseFieldItems(items []string) map[string]any {
	fields := map[string]any{}
	for _, item := range items {
		name, value := b.parseFieldItem(item)
		fields[name] = value
	}
	return fields
}

func (b *LoggerBuilder) parseFieldItem(item string) (name string, value any) {
	switch item {
	case pidLogFieldValue:
		name = pidLogFieldName
		value = pidLogFieldValue
	default:
		equals := strings.Index(item, "=")
		if equals != -1 {
			name = item[0:equals]
			value = item[equals+1:]
		} else {
			name = item
			value = ""
		}
		name = strings.TrimSpace(name)
	}
	return
}

// Build uses the data stored in the buider to create a new logger.
func (b *LoggerBuilder) Build() (result *slog.Logger, err error) {
	// If no writer has been explicitly provided then open the log file:
	writer := b.writer
	if writer == nil {
		writer, err = b.openWriter()
		if err != nil {
			return
		}
	}

	// Map the level to a slog level:
	level := slog.LevelInfo
	if b.level != "" {
		err = level.UnmarshalText([]byte(b.level))
		if err != nil {
			return
		}
	}

	// Create the handler:
	replacers := make([]func([]string, slog.Attr) slog.Attr, 0, 2)
	replacers = append(replacers, replaceTime)
	if b.redact {
		replacers = append(replacers, replaceRedacted)
	} else {
		replacers = append(replacers, preserveRedacted)
	}
	options := &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: composeReplacers(replacers),
	}
	handler := slog.NewJSONHandler(writer, options)

	// Caculate the custom fields:
	fields, err := b.customFields()
	if err != nil {
		return
	}

	// Create the logger:
	result = slog.New(handler).With(fields...)

	return
}

func (b *LoggerBuilder) openWriter() (result io.Writer, err error) {
	switch b.file {
	case "", "stdout":
		if b.out != nil {
			result = b.out
		} else {
			result = os.Stdout
		}
	case "stderr":
		if b.err != nil {
			result = b.err
		} else {
			result = os.Stderr
		}
	default:
		result, err = b.openFile(b.file)
	}
	return
}

func (b *LoggerBuilder) openFile(file string) (result io.Writer, err error) {
	result, err = os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0660)
	return
}

func (b *LoggerBuilder) customFields() (result []any, err error) {
	names := make([]string, len(b.fields))
	i := 0
	for name := range b.fields {
		names[i] = name
		i++
	}
	sort.Strings(names)
	fields := make([]any, 2*len(names))
	for i, name := range names {
		value := b.fields[name]
		fields[2*i] = name
		fields[2*i+1], err = b.customField(name, value)
		if err != nil {
			return
		}
	}
	result = fields
	return
}

func (b *LoggerBuilder) customField(name string, value any) (result any, err error) { // nolint: unparam
	switch value {
	case pidLogFieldValue:
		result = os.Getpid()
	default:
		result = value
	}
	return
}

func composeReplacers(replacers []func([]string, slog.Attr) slog.Attr) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		for _, replacer := range replacers {
			a = replacer(groups, a)
		}
		return a
	}
}

func replaceTime(groups []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindTime {
		value := a.Value.Time().UTC()
		a = slog.String(a.Key, value.Format(time.RFC3339))
	}
	return a
}

func replaceRedacted(groups []string, a slog.Attr) slog.Attr {
	if strings.HasPrefix(a.Key, "!") {
		a = slog.String(a.Key[1:], "***")
	}
	return a
}

func preserveRedacted(groups []string, a slog.Attr) slog.Attr {
	a.Key = strings.TrimPrefix(a.Key, "!")
	return a
}

// Values of log fields with special meanings. For example '%p' will be replaced with the identifier
// of the process.
const (
	pidLogFieldName  = "pid"
	pidLogFieldValue = "%p"
)
