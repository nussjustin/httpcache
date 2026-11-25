// Package cachecontrol implements tokenization and parsing of Cache-Control header directives based on a relaxed
// version of RFC 9111, similar to the implementations in major web browser engines (Chromium, Firefox, Safari).
package cachecontrol

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"github.com/nussjustin/httpsfv"
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

// Parser parses a Cache-Control value and returns a sequence of directives.
//
// This is implemented as a struct with methods and not as iter.Seq2 in order to avoid allocations.
type Parser struct {
	dir Directive
	err error

	// for structured fields
	iterNext func() (string, httpsfv.ItemOrInnerList, bool)
	iterDone func()

	toks  Tokenizer
	state parserState
}

type parserState uint8

const (
	stateName parserState = iota
	stateValue
)

// NewParser returns a Parser that parses a Cache-Control header as defined in RFC 9111, while trying its best to form
// directives even from non RFC 9111 compliant inputs.
//
// Notably, a directive like `directive1=value "with" space,directive2` will _correctly_ parse the first directive with
// the value `value "with" space`.
func NewParser(s string) Parser {
	return Parser{toks: Tokenizer{input: s}, state: stateName}
}

// ParseStructured is like [NewParser], but parses the header as a structured field, as defined in RFC 9651 and as
// required when parsing a targeted HTTP cache control header as specified by RFC 9213.
func NewParserForStructured(s string) (Parser, error) {
	dict, err := httpsfv.Parse[httpsfv.Dictionary](s)
	if err != nil {
		return Parser{}, err
	}
	next, done := iter.Pull2(dict.All())
	return Parser{iterNext: next, iterDone: done}, nil
}

// Directive returns the directive parsed during the last call to Next.
func (p *Parser) Directive() Directive {
	return p.dir
}

// Err returns the error encountered during the last call to Next.
func (p *Parser) Err() error {
	return p.err
}

// Next parses the next directive.
//
// It returns false if the input has been fully consumed, or true otherwise.
//
// Note that this can return true even if there was an error and that another call to Next could again succeed and
// reset the error.
func (p *Parser) Next() bool {
	if p.iterNext != nil {
		return p.nextStructured()
	}
	return p.next()
}

func (p *Parser) next() bool {
	var name string
	var value string

	lastToken := p.toks.Token()

	var found bool

	for !found && p.toks.Next() {
		token := p.toks.Token()

		switch p.state {
		case stateName:
			switch token.Type {
			case TokenTypeComma:
				if name == "" {
					break
				}

				p.dir, found = Directive{Name: name}, true

				name = ""
			case TokenTypeEquals:
				p.state = stateValue
			case TokenTypeSpace:
				// Do nothing
			case TokenTypeText:
				if name != "" && lastToken.Type == TokenTypeSpace {
					name += lastToken.Text
				}

				name += token.Text
			case TokenTypeInvalid:
				panic("unreachable")
			}
		case stateValue:
			switch token.Type {
			case TokenTypeComma:
				p.dir, found = Directive{Name: name, Value: value, HasValue: true}, true

				name, value = "", ""

				p.state = stateName
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
			case TokenTypeInvalid:
				panic("unreachable")
			}
		}

		lastToken = token
	}

	if found {
		return found
	}

	if p.state == stateName && name == "" {
		return false
	}

	p.dir = Directive{Name: name, Value: value, HasValue: p.state == stateValue}
	p.state = stateName

	return true
}

func (p *Parser) nextStructured() bool {
	if p.iterDone == nil {
		return false
	}

	for {
		k, v, ok := p.iterNext()
		if !ok {
			p.iterDone()
			p.iterDone = nil
			return false
		}

		if v.Type() == httpsfv.ItemOrInnerListTypeInnerList {
			p.dir, p.err = Directive{}, fmt.Errorf("directive %q must be an item", k)
			return true
		}

		i := v.Item()

		d := Directive{Name: k}

		switch i.Type() {
		case httpsfv.BareItemTypeBoolean:
			if i.Boolean() {
				break
			}

			// Treat a false as not being set
			continue
		case httpsfv.BareItemTypeInteger:
			d.Value, d.HasValue = strconv.FormatInt(i.Integer(), 10), true
		case httpsfv.BareItemTypeString:
			d.Value, d.HasValue = i.String(), true
		case httpsfv.BareItemTypeToken:
			d.Value, d.HasValue = i.Token(), true
		case httpsfv.BareItemTypeInvalid,
			httpsfv.BareItemTypeByteSequence,
			httpsfv.BareItemTypeDate,
			httpsfv.BareItemTypeDecimal,
			httpsfv.BareItemTypeDisplayString:
			p.dir, p.err = Directive{}, fmt.Errorf("directive %q has unexpected type %s", k, i.Type())
			return true
		}

		p.dir, p.err = d, nil
		return true
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

// Tokenizer takes a string of Cache-Control directives and returns a sequence of tokens.
//
// For quoted strings, it will return a token containing the unquoted, unescaped text.
//
// For quoted strings without ending quoted, it will read until the next comma or equals sign or the end of the string.
//
// This is implemented as a struct with methods and not as iter.Seq in order to avoid allocations.
type Tokenizer struct {
	input string
	off   int
	tok   Token
}

// NewTokenizer returns a new Tokenizer for the given string.
func NewTokenizer(input string) Tokenizer {
	return Tokenizer{input: input}
}

// Next reads the next token.
//
// It returns false if no more tokens are available.
func (t *Tokenizer) Next() bool {
	var found bool

	i, s := t.off, t.input
	textStart := -1

loop:
	for ; i < len(s) && !found; i++ {
		c := s[i]

		switch {
		case isControlCharacterOrSpace(c):
			if textStart != -1 && textStart < i {
				t.tok, found = Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}, true
				textStart = -1
				break loop
			}

			j := i + 1

			for j < len(s) {
				if !isControlCharacterOrSpace(s[j]) {
					break
				}

				j++
			}

			t.tok, found = Token{Type: TokenTypeSpace, Start: i, End: j, Text: s[i:j]}, true

			i = j - 1
		case c == ',':
			if textStart != -1 && textStart < i {
				t.tok, found = Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}, true
				textStart = -1
				break loop
			}

			t.tok, found = Token{Type: TokenTypeComma, Start: i, End: i + 1, Text: ","}, true
		case c == '=':
			if textStart != -1 && textStart < i {
				t.tok, found = Token{Type: TokenTypeText, Start: textStart, End: i, Text: s[textStart:i]}, true
				textStart = -1
				break loop
			}

			t.tok, found = Token{Type: TokenTypeEquals, Start: i, End: i + 1, Text: "="}, true
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

					t.tok, found = Token{Type: TokenTypeText, Start: i, End: j + 1, Text: text}, true

					// Skip past the closing quote
					i = j + 1

					break loop
				}
			}

			// No closing quote found before end of string, treat the starting quote as normal character
			fallthrough
		case textStart == -1:
			textStart = i
		}
	}

	t.off = i

	if !found && textStart != -1 {
		t.tok = Token{Type: TokenTypeText, Start: textStart, End: len(s), Text: s[textStart:]}
		found = true
	}

	return found
}

// Token returns the last read token.
func (t *Tokenizer) Token() Token {
	return t.tok
}

func isControlCharacterOrSpace(c byte) bool {
	return c <= ' ' || c == 127
}
