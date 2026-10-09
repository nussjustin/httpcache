package httpcache

import (
	"bytes"
	"errors"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nussjustin/httpsfv"
)

// URLVariationConfig is the parsed representation of the No-Vary-Search header as specified in
// draft-ietf-httpbis-no-vary-search-10.
type URLVariationConfig struct {
	// VaryOnKeyOrder, if true, means that a different order of keys in a query string should result in separate cache
	// entries, even if the keys and their values are otherwise equals.
	VaryOnKeyOrder bool

	// NoVaryParams contains the list of parameters which should not result in different cache entries.
	NoVaryParams []string

	// NoVaryParamsWildcard, if true, means that all parameters should be ignored when matching cache entries.
	NoVaryParamsWildcard bool

	// VaryParams contains the list of parameters by which cache entries should vary.
	VaryParams []string

	// VaryParamsWildcard, if true, means that all parameters should be used when matching cache entries.
	VaryParamsWildcard bool
}

// DefaultURLVariationConfig is a URL variation config whose no-vary params is an empty list, vary params is
// wildcard, and vary on key order is true.
var DefaultURLVariationConfig = URLVariationConfig{
	VaryOnKeyOrder:     true,
	VaryParamsWildcard: true,
}

func isDefaultURLVariationConfig(u URLVariationConfig) bool {
	// See comment on DefaultURLVariationConfig.
	return len(u.NoVaryParams) == 0 && !u.NoVaryParamsWildcard && u.VaryParamsWildcard && u.VaryOnKeyOrder
}

// ParseNoVarySearch parses the given No-Vary-Search response header lines.
func ParseNoVarySearch(lines []string) (URLVariationConfig, error) {
	value, err := httpsfv.ParseLines[httpsfv.Dictionary](lines)

	// From https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-parse-a-url-variation-confi
	//
	// 1. If value is null, then return the default URL variation config.
	if err != nil {
		return DefaultURLVariationConfig, err
	}

	// 2. Let result be a new URL variation config.
	var result URLVariationConfig

	// 3. Set result's vary on key order to true.
	result.VaryOnKeyOrder = true

	// 4. If value["key-order"] exists:
	if v, ok := value.Get("key-order"); ok {
		// 1. Let keyOrderValue be the item_or_inner_list component of the tuple value["key-order"] (ignoring any parameters).
		keyOrderValue := v

		// 2. If keyOrderValue is not a boolean, then return the default URL variation config.
		if keyOrderValue.Type() != httpsfv.ItemOrInnerListTypeItem {
			return DefaultURLVariationConfig, errors.New("key-order must be a boolean")
		}

		keyOrderValueItem := keyOrderValue.Item()

		if keyOrderValueItem.Type() != httpsfv.BareItemTypeBoolean {
			return DefaultURLVariationConfig, errors.New("key-order must be a boolean")
		}

		// 3. Set result's vary on key order to the boolean negation of keyOrderValue.
		result.VaryOnKeyOrder = !keyOrderValueItem.Boolean()
	}

	paramsValue, paramsOk := value.Get("params")
	exceptValue, exceptOk := value.Get("except")

	switch {
	// 5. If both value["params"] and value["except"] exist, then return the default URL variation config.
	case paramsOk && exceptOk:
		return DefaultURLVariationConfig, errors.New("except and params set at the same time")
	// 6. If neither value["params"] nor value["except"] exists:
	case !paramsOk && !exceptOk:
		// 1. Set result's no-vary params to an empty list.
		result.NoVaryParams = nil

		// 2. Set result's vary params to wildcard.
		result.VaryParamsWildcard = true
	// 7. If value["params"] exists:
	case paramsOk:
		// 1. Let paramsValue be the item_or_inner_list component of the tuple value["params"] (ignoring any parameters).
		_ = paramsValue

		// 2. If paramsValue is not an inner list, then return the default URL variation config.
		if paramsValue.Type() != httpsfv.ItemOrInnerListTypeInnerList {
			return DefaultURLVariationConfig, errors.New("params must be an inner-list")
		}

		// 3. Let paramsList be a list containing the bare_item component of each tuple in paramsValue (ignoring any parameters).
		paramsList := paramsValue.InnerList().Members

		var keys []string
		if len(paramsList) > 0 {
			keys = make([]string, len(paramsList))
		}

		// 4. If any item in paramsList is not a string, then return the default URL variation config.
		for i, param := range paramsList {
			if param.Type() != httpsfv.BareItemTypeString {
				return DefaultURLVariationConfig, errors.New("params must all be strings")
			}

			keys[i] = parseNoVaryKey(param.String())
		}

		// 5. Set result's no-vary params to the result of applying parse a key (Section 5.3) to each item in paramsList.
		result.NoVaryParams = keys

		// 6. Set result's vary params to wildcard.
		result.VaryParamsWildcard = true
	// 8. Otherwise, if value["except"] exists:
	default:
		// 1. Let exceptValue be the item_or_inner_list component of the tuple value["except"] (ignoring any parameters).
		_ = exceptValue

		// 2. If exceptValue is not an inner list, then return the default URL variation config.
		if exceptValue.Type() != httpsfv.ItemOrInnerListTypeInnerList {
			return DefaultURLVariationConfig, errors.New("except must be an inner-list")
		}

		// 3. Let exceptList be a list containing the bare_item component of each tuple in exceptValue (ignoring any parameters).
		exceptList := exceptValue.InnerList().Members

		var keys []string
		if len(exceptList) > 0 {
			keys = make([]string, len(exceptList))
		}

		// 4. If any item in exceptList is not a string, then return the default URL variation config.
		for i, except := range exceptList {
			if except.Type() != httpsfv.BareItemTypeString {
				return DefaultURLVariationConfig, errors.New("except must all be strings")
			}

			keys[i] = parseNoVaryKey(except.String())
		}

		// 5. Set result's vary params to the result of applying parse a key (Section 5.3) to each item in exceptList.
		result.VaryParams = keys

		// 6. Set result's no-vary params to wildcard.
		result.NoVaryParamsWildcard = true
	}

	// 9. Return result.
	return result, nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexDecode(b byte) byte {
	if b >= '0' && b <= '9' {
		return b - '0'
	}

	if b >= 'a' && b <= 'f' {
		return b - 'a' + 10
	}

	if b >= 'A' && b <= 'F' {
		return b - 'A' + 10
	}

	panic("invalid hex char")
}

func parseNoVaryKey(keyString string) string {
	if strings.IndexByte(keyString, '+') == -1 && strings.IndexByte(keyString, '%') == -1 {
		return keyString
	}

	// From https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-parse-a-key
	//
	// 1. Let keyBytes be the isomorphic encoding [WHATWG-INFRA] of keyString.
	keyBytes := []byte(keyString)

	// 2. Replace any 0x2B (+) in keyBytes with 0x20 (SP).
	keyBytes = bytes.ReplaceAll(keyBytes, []byte{'+'}, []byte{' '})

	// 3. Let keyBytesDecoded be the percent-decoding [WHATWG-URL] of keyBytes.
	keyBytes = percentDecode(keyBytes)

	// 4. Let keyStringDecoded be the UTF-8 decoding without BOM [WHATWG-ENCODING] of keyBytesDecoded.
	keyStringDecoded := utf8DecodeWithoutBOMString(string(keyBytes))

	// 5. Return keyStringDecoded.
	return keyStringDecoded
}

type urlDecodedPair [2]string
type urlDecodedPairs []urlDecodedPair

func (ps urlDecodedPairs) except(params []string) urlDecodedPairs {
	ps2 := ps[:0]

	for _, p := range ps {
		if slices.Contains(params, p[0]) {
			continue
		}

		ps2 = append(ps2, p)
	}

	return ps2
}

func (ps urlDecodedPairs) only(params []string) urlDecodedPairs {
	ps2 := ps[:0]

	for _, p := range ps {
		if !slices.Contains(params, p[0]) {
			continue
		}

		ps2 = append(ps2, p)
	}

	return ps2
}

func parseURLEncoded(input string) urlDecodedPairs {
	if input == "" {
		return nil
	}

	// From https://url.spec.whatwg.org/#concept-urlencoded-parser
	//
	// 1. Let sequences be the result of splitting input on 0x26 (&).
	sequences := strings.SplitSeq(input, "&")

	// 2. Let output be an initially empty list of name-value tuples where both name and value hold a string.
	output := make(urlDecodedPairs, 0, strings.Count(input, "&"))

	// 3. For each byte sequence bytes in sequences:
	for bytes_ := range sequences {
		// 1. If bytes is the empty byte sequence, then continue.
		if bytes_ == "" {
			continue
		}

		var name, value string

		equalsIdx := strings.Index(bytes_, "=")

		switch {
		// 2. If bytes contains a 0x3D (=), then let name be the bytes from the start of bytes up to but excluding
		//    its first 0x3D (=), and let value be the bytes, if any, after the first 0x3D (=) up to the end of bytes.
		//    If 0x3D (=) is the first byte, then name will be the empty byte sequence. If it is the last, then value
		//    will be the empty byte sequence.
		case equalsIdx != -1:
			name, value = bytes_[:equalsIdx], bytes_[equalsIdx+1:]
		// 3. Otherwise, let name have the value of bytes and let value be the empty byte sequence.
		default:
			name = bytes_
		}

		// 4. Replace any 0x2B (+) in name and value with 0x20 (SP).
		name = strings.ReplaceAll(name, "+", " ")
		value = strings.ReplaceAll(value, "+", " ")

		// 5. Let nameString and valueString be the result of running UTF-8 decode without BOM on the percent-decoding
		//    of name and value, respectively.
		nameString := utf8DecodeWithoutBOMString(percentDecodeString(name))
		valueString := utf8DecodeWithoutBOMString(percentDecodeString(value))

		// 6. Append (nameString, valueString) to output.
		output = append(output, urlDecodedPair{nameString, valueString})
	}

	// 4. Return output.
	return output
}

func percentDecode(input []byte) []byte {
	// From https://url.spec.whatwg.org/#percent-encoded-bytes
	//
	// 1. Let output be an empty byte sequence.
	output := input[:0]

	// 2. For each byte byte in input:
	for i := 0; i < len(input); i++ {
		byte_ := input[i]

		switch {
		// 1. If byte is not 0x25 (%), then append byte to output.
		case byte_ != '%':
			output = append(output, byte_)
		// 2. Otherwise, if byte is 0x25 (%) and the next two bytes after byte in input are not in the ranges 0x30 (0)
		//    to 0x39 (9), 0x41 (A) to 0x46 (F), and 0x61 (a) to 0x66 (f), all inclusive, append byte to output.
		case i+2 >= len(input) || !isHex(input[i+1]) || !isHex(input[i+2]):
			output = append(output, '%')
		// 3. Otherwise:
		default:
			// 1. Let bytePoint be the two bytes after byte in input, decoded, and then interpreted as a hexadecimal number.
			bytePoint := hexDecode(input[i+1])<<4 + hexDecode(input[i+2])

			// 2. Append a byte whose value is bytePoint to output.
			output = append(output, bytePoint)

			// 3. Skip the next two bytes in input.
			i += 2
		}
	}

	// 3. Return output.
	return output
}

func percentDecodeString(input string) string {
	if strings.IndexByte(input, '%') == -1 {
		return input
	}

	return string(percentDecode([]byte(input)))
}

func utf8DecodeWithoutBOMString(input string) string {
	if r, sz := utf8.DecodeRuneInString(input); r == '\uFEFF' {
		input = input[sz:]
	}

	var i int
	for i < len(input) {
		r, sz := utf8.DecodeRuneInString(input[i:])
		if r != utf8.RuneError {
			i += sz
			continue
		}

		input = input[:i] + string(utf8.RuneError) + input[i+sz:]
		i += utf8.UTFMax
	}

	return input
}

// Equals returns true if urlA and urlB compare equal under u.
func (u *URLVariationConfig) Equals(urlA, urlB *url.URL) bool {
	variationConfig := *u

	// From https://httpwg.org/http-extensions/draft-ietf-httpbis-no-vary-search.html#name-comparing
	//
	// Two URLs [WHATWG-URL] urlA and urlB are equivalent modulo variation config given a URL variation config
	// variationConfig if the following algorithm returns true:
	//
	// 1. If the scheme, host, port, or path of urlA and urlB differ, then return false.
	if urlA.Scheme != urlB.Scheme || urlA.Host != urlB.Host || urlA.EscapedPath() != urlB.EscapedPath() {
		return false
	}

	// 2. If variationConfig is equivalent to the default URL variation config, then:
	if isDefaultURLVariationConfig(variationConfig) {
		// 1. If urlA's query equals urlB's query, then return true.
		if urlA.RawQuery == urlB.RawQuery || slices.Equal(parseURLEncoded(urlA.RawQuery), parseURLEncoded(urlB.RawQuery)) {
			return true
		}

		// 2. Return false.
		return false
	}

	// 3. Let searchParamsA and searchParamsB be empty lists.
	var searchParamsA, searchParamsB urlDecodedPairs

	// 4. If urlA's query is not null, then set searchParamsA to the result of running the
	//    application/x-www-form-urlencoded parser [WHATWG-URL] given the isomorphic encoding [WHATWG-INFRA] of
	//    urlA's query.
	if urlA.RawQuery != "" {
		searchParamsA = parseURLEncoded(urlA.RawQuery)
	}

	// 5. If urlB's query is not null, then set searchParamsB to the result of running the
	//    application/x-www-form-urlencoded parser [WHATWG-URL] given the isomorphic encoding [WHATWG-INFRA] of
	//    urlB's query.
	if urlB.RawQuery != "" {
		searchParamsB = parseURLEncoded(urlB.RawQuery)
	}

	switch {
	// 6. If variationConfig's no-vary params is a list, then:
	case !variationConfig.NoVaryParamsWildcard:
		// 1. Set searchParamsA to a list containing those items pair in searchParamsA where variationConfig's
		//    no-vary params does not contain pair[0].
		searchParamsA = searchParamsA.except(variationConfig.NoVaryParams)

		// 2. Set searchParamsB to a list containing those items pair in searchParamsB where variationConfig's
		//    no-vary params does not contain pair[0].
		searchParamsB = searchParamsB.except(variationConfig.NoVaryParams)
	// 7. Otherwise, if variationConfig's vary params is a list, then:
	case !variationConfig.VaryParamsWildcard:
		// 1. Set searchParamsA to a list containing those items pair in searchParamsA where variationConfig's
		//    vary params contains pair[0].
		searchParamsA = searchParamsA.only(variationConfig.VaryParams)

		// 2. Set searchParamsB to a list containing those items pair in searchParamsB where variationConfig's
		//    vary params contains pair[0].
		searchParamsB = searchParamsB.only(variationConfig.VaryParams)
	}

	// 8. If variationConfig's vary on key order is false, then:
	if !variationConfig.VaryParamsWildcard {
		// 1. Let keyLessThan be an algorithm taking as inputs two pairs (keyA, valueA) and (keyB, valueB), which
		//    returns whether keyA is code unit less than [WHATWG-INFRA] keyB.
		keyLessThan := func(a, b urlDecodedPair) int {
			return strings.Compare(a[0], b[0])
		}

		// 2. Set searchParamsA to the result of sorting [WHATWG-INFRA] searchParamsA in ascending order with keyLessThan.
		slices.SortFunc(searchParamsA, keyLessThan)

		// 3. Set searchParamsB to the result of sorting [WHATWG-INFRA] searchParamsB in ascending order with keyLessThan.
		slices.SortFunc(searchParamsB, keyLessThan)
	}

	// The following are collapsed into the slices.Equal call:
	// 9. If searchParamsA's size is not equal to searchParamsB's size, then return false.
	// 10. Let i be 0.
	// 11. While i < searchParamsA's size:
	// 11. 1. If searchParamsA[i][0] does not equal searchParamsB[i][0], then return false.
	// 11. 2. If searchParamsA[i][1] does not equal searchParamsB[i][1], then return false.
	// 11. 3. Set i to i + 1.
	// 12. Return true
	return slices.Equal(searchParamsA, searchParamsB)
}
