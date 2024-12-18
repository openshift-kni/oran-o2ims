package api

import (
	"fmt"
	"net/url"
)

// ValidateCallbackURL ensures that the URL used in subscription callback meets our requirements
func ValidateCallbackURL(callback string) error {
	u, err := url.Parse(callback)
	if err != nil {
		return fmt.Errorf("invalid callback URL: %w", err)
	}

	if u.Scheme != "https" {
		return fmt.Errorf("invalid callback scheme %q, only https is supported", u.Scheme)
	}

	return nil
}
