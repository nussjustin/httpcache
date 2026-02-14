// Package cachecontrol implements tokenization and parsing of Cache-Control header directives based on a relaxed
// version of RFC 9111, similar to the implementations in major web browser engines (Chromium, Firefox, Safari).
package cachecontrol

import (
	"iter"
	"strings"
)

// Directive represents a single Cache-Control directive with optional value.
type Directive struct {
	// Name of the directive. May be empty if HasValue is true.
	Name string

	// Value of the directive, if any. May be empty. Check HasValue to differentiate between an empty and no value.
	Value string

	// HasValue is true if Value is set.
	HasValue bool
}

// Parse parses the given Cache-Control value and returns a sequence of directives.
//
// The parsing uses [Tokenize] to extract tokens from the given string and tries its best to form directives even from
// non RFC9111 compliant inputs.
//
// Notably a directive like `directive1=value "with" space,directive2` will _correctly_ parse the first directive with
// the value `value "with" space`.
func Parse(s string) iter.Seq[Directive] {
	return func(yield func(Directive) bool) {
		const (
			stateName = iota
			stateValue
		)

		state := stateName

		var name string
		var value string

		var lastToken Token

		for token := range Tokenize(s) {
			switch state {
			case stateName:
				switch token.Type {
				case TokenTypeComma:
					if name == "" {
						break
					}

					if !yield(Directive{Name: name}) {
						return
					}

					name = ""
				case TokenTypeEquals:
					state = stateValue
				case TokenTypeSpace:
					// Do nothing
				case TokenTypeText:
					if name != "" && lastToken.Type == TokenTypeSpace {
						name += lastToken.Text
					}

					name += token.Text
				default:
					panic("unreachable")
				}
			case stateValue:
				switch token.Type {
				case TokenTypeComma:
					if !yield(Directive{Name: name, Value: value, HasValue: true}) {
						return
					}

					name, value = "", ""

					state = stateName
				case TokenTypeEquals:
					if value != "" && lastToken.Type == TokenTypeSpace {
						value += lastToken.Text
					}

					value += token.Text
				case TokenTypeSpace:
					// Do nothing
				case TokenTypeText:
					if value != "" && lastToken.Type == TokenTypeSpace {
						value += lastToken.Text
					}

					value += token.Text
				default:
					panic("unreachable")
				}
			}

			lastToken = token
		}

		if state == stateName && name == "" {
			return
		}

		yield(Directive{Name: name, Value: value, HasValue: state == stateValue})
	}
}

// Token represents a parsed token from a string of Cache-Control directives.
type Token struct {
	// Type is the type of the token.
	Type TokenType

	// Start is the byte offset in the input string where the token starts.
	Start int

	// End is the byte offset in the input string where the token ends.
	End int

	// Text is the text of the token.
	//
	// For tokens of type [TokenTypeText], if the parsed text was a quoted string, this contains the unescaped string
	// without quotes.
	//
	// For [TokenTypeComma] and [TokenTypeEquals] this contains a single "," or "=".
	//
	// For [TokenTypeSpace] this contains the spaces, including control characters.
	Text string
}

// TokenType is an enum of token types as understood by [Tokenize].
type TokenType uint8

const (
	// TokenTypeInvalid is the zero value of [TokenType] and is not a valid value.
	// [Tokenize] will never return a token of this type.
	TokenTypeInvalid TokenType = iota

	// TokenTypeComma represents a single comma.
	TokenTypeComma

	// TokenTypeEquals represents a single equals sign.
	TokenTypeEquals

	// TokenTypeSpace represents one or more spaces or control characters.
	TokenTypeSpace

	// TokenTypeText represents a text value. This can be either a quoted-string or an unquoted value.
	TokenTypeText
)

// String implements the [fmt.Stringer] interface.
func (t TokenType) String() string {
	switch t {
	case TokenTypeInvalid:
		return "invalid"
	case TokenTypeComma:
		return "comma"
	case TokenTypeEquals:
		return "equals"
	case TokenTypeSpace:
		return "space"
	case TokenTypeText:
		return "text"
	}

	panic("invalid TokenType")
}

// Tokenize takes a string of Cache-Control directives and returns a sequence of tokens.
//
// For quoted strings, it will return a token containing the unquoted, unescaped text.
//
// For quoted strings without ending quoted, it will read until the next comma or the end of the string.
func Tokenize(s string) iter.Seq[Token] {
	return func(yield func(Token) bool) {
		textStart := -1

	next:
		for i := 0; i < len(s); i++ {
			c := s[i]

			switch {
			case isControlCharacterOrSpace(c):
				if textStart != -1 && textStart < i {
					if !yield(Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}) {
						return
					}

					textStart = -1
				}

				j := i + 1

				for j < len(s) {
					if !isControlCharacterOrSpace(s[j]) {
						break
					}

					j++
				}

				if !yield(Token{Type: TokenTypeSpace, Start: i, End: j, Text: s[i:j]}) {
					return
				}

				i = j - 1
			case c == ',':
				if textStart != -1 && textStart < i {
					if !yield(Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}) {
						return
					}

					textStart = -1
				}

				if !yield(Token{Type: TokenTypeComma, Start: i, End: i + 1, Text: ","}) {
					return
				}
			case c == '=':
				if textStart != -1 && textStart < i {
					if !yield(Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}) {
						return
					}

					textStart = -1
				}

				if !yield(Token{Type: TokenTypeEquals, Start: i, End: i + 1, Text: "="}) {
					return
				}
			case textStart == -1 && c == '"':
				var escaping bool
				var unescaped strings.Builder
				unescapedTo := i + 1

				for j := i + 1; j < len(s); j++ {
					switch {
					case escaping:
						unescaped.WriteByte(s[j])
						unescapedTo = j + 1

						escaping = false
					case s[j] == '\\':
						if unescapedTo < j {
							unescaped.WriteString(s[unescapedTo:j])
							unescapedTo = j + 1
						}

						escaping = true
					case s[j] == '"':
						var text string

						if unescaped.Len() > 0 {
							unescaped.WriteString(s[unescapedTo:j])

							text = unescaped.String()
						} else {
							text = s[i+1 : j]
						}

						if !yield(Token{Type: TokenTypeText, Start: i, End: j + 1, Text: text}) {
							return
						}

						// Will be incremented for the next loop iteration, so no + 1
						i = j

						continue next
					}
				}

				fallthrough
			case textStart == -1:
				textStart = i
			}
		}

		if textStart != -1 {
			yield(Token{Type: TokenTypeText, Start: textStart, End: len(s), Text: s[textStart:]})
		}
	}
}

func isControlCharacterOrSpace(c byte) bool {
	return c <= ' ' || c == 127
}
