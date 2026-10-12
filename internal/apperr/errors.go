package apperr

import (
	"errors"
	"log/slog"
)

type errWithAttrs struct {
	error
	attrs []slog.Attr
}

func WithAttrs(err error, args ...any) error {
	if err == nil {
		return nil
	}
	return &errWithAttrs{error: err, attrs: argsToAttr(args)}
}

func argsToAttr(args []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args))
	for i := 0; i < len(args); {
		switch key := args[i].(type) {
		case slog.Attr:
			attrs = append(attrs, key)
			i++
		case string:
			if i+1 >= len(args) {
				attrs = append(attrs, slog.String("!BADKEY", key))
				i++
			} else {
				attrs = append(attrs, slog.Any(key, args[i+1]))
				i += 2
			}
		default:
			attrs = append(attrs, slog.Any("!BADKEY", args[i]))
			i++
		}
	}
	return attrs
}

func (e *errWithAttrs) Unwrap() error      { return e.error }
func (e *errWithAttrs) Attrs() []slog.Attr { return e.attrs }

type attrError interface{ Attrs() []slog.Attr }

// Attrs extracts logging attributes from outermost to innermost errors.
func Attrs(err error) []slog.Attr {
	var attrs []slog.Attr
	seen := map[string]bool{}
	for err != nil {
		if ae, ok := err.(attrError); ok {
			for _, a := range ae.Attrs() {
				if !seen[a.Key] {
					attrs = append(attrs, a)
					seen[a.Key] = true
				}
			}
		}
		err = errors.Unwrap(err)
	}
	return attrs
}
