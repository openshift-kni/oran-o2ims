/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package logging

import "github.com/spf13/pflag"

// AddFlags adds the flags related to logging to the given flag set.
func AddFlags(set *pflag.FlagSet) {
	_ = set.String(
		levelFlagName,
		"info",
		"Log level. Possible values are 'debug', 'info', 'warn' and 'error'. The "+
			"default is 'info.",
	)
	_ = set.String(
		fileFlagName,
		"stdout",
		"Log file. The value can also be 'stdout' or 'stderr' and then the log will be "+
			"written to the standard output or error stream of the process.",
	)
	_ = set.StringArray(
		fieldFlagName,
		[]string{},
		"Field to add to all log messages. The value can be a percent sign followed by "+
			"one of the letters that indicate a special value, or a field name "+
			"followed by an equals sign and the field value. For example '%p' "+
			"results in a field named 'pid' containing the identifier of the "+
			"process, and 'my-field=my-value' results in adding a field named "+
			"'my-field' with value 'my-value'.",
	)
	_ = set.StringSlice(
		fieldsFlagName,
		[]string{},
		"Comma separated list of fields to add to all log messages. See the "+
			"'--log-field' option for details of allowed values. Note that "+
			"this doesn't allow values containing commas, use the '--log-field' "+
			"option if you need that.",
	)
	_ = set.Bool(
		headersFlagName,
		false,
		"Include HTTP headers in log messages.",
	)
	_ = set.Bool(
		bodiesFlagName,
		false,
		"Include details of HTTP request and response bodies in log messages. Note "+
			"that currently only the size is written, not the complete body.",
	)
	_ = set.Bool(
		redactFlagName,
		true,
		"Enables or disables redactiong security sensitive data from the log.",
	)
}

// Names of the flags:
const (
	levelFlagName   = "log-level"
	fileFlagName    = "log-file"
	fieldFlagName   = "log-field"
	fieldsFlagName  = "log-fields"
	headersFlagName = "log-headers"
	bodiesFlagName  = "log-bodies"
	redactFlagName  = "log-redact"
)
