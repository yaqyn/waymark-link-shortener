package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Validate syntax without making an outbound request. A shortener should not
// fetch user-supplied destinations (SSRF), or depend on their current availability.
func validateDestination(ctx context.Context, raw string) (string, error) {
	_, span := tracer.Start(ctx, "link.validate_destination")
	defer span.End()
	return normalizeDestination(raw)
}

func normalizeDestination(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 4096 {
		return "", errors.New("enter a URL of at most 4096 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Opaque != "" {
		return "", errors.New("enter a complete http:// or https:// URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("only HTTP and HTTPS URLs are supported")
	}
	if u.User != nil {
		return "", errors.New("URLs containing usernames or passwords are not allowed")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("URL port must be between 1 and 65535")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("URL port must not be empty")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return "", errors.New("URL contains invalid characters")
	}
	return u.String(), nil
}
