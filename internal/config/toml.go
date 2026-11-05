package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits from FR-052: a configuration file and a single line may not exceed
// one mebibyte.
const (
	maxConfigBytes = 1 << 20
	maxLineBytes   = 1 << 20
)

// tomlKind is the closed set of value types the subset understands.
type tomlKind uint8

const (
	tomlString tomlKind = iota
	tomlInteger
	tomlBool
	tomlArray
	tomlTableValue
	tomlBare
)

func (k tomlKind) String() string {
	switch k {
	case tomlString:
		return "string"
	case tomlInteger:
		return "integer"
	case tomlBool:
		return "boolean"
	case tomlArray:
		return "array"
	case tomlTableValue:
		return "inline table"
	case tomlBare:
		return "bare value"
	}
	return "value"
}

type tomlValue struct {
	kind    tomlKind
	str     string
	integer int64
	boolean bool
	array   []tomlValue
	table   []tomlEntry
	line    int
}

type tomlEntry struct {
	key   string
	line  int
	value tomlValue
}

type tomlTable struct {
	name    string
	line    int
	entries []tomlEntry
}

type tomlDoc struct {
	path   string
	top    []tomlEntry
	tables []tomlTable
}

// parseDocument reads the whole supported TOML subset. Bare scalars are
// carried only for the ten rev-5 top-level keys (FR-056); every other position
// stays strict TOML, so a bare value there is a parse error.
func parseDocument(path string, data []byte) (*tomlDoc, error) {
	if len(data) > maxConfigBytes {
		if line := firstOversizedLine(data); line > 0 {
			return nil, &Error{Path: path, Line: line, Message: "line is longer than 1 MiB"}
		}
		return nil, fileError(path, "configuration file is larger than 1 MiB")
	}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		return nil, fileError(path, "byte-order mark (BOM) is not supported")
	}
	if !utf8.Valid(data) {
		return nil, fileError(path, "configuration is not valid UTF-8")
	}

	p := &parser{
		path:     path,
		doc:      &tomlDoc{path: path},
		explicit: map[string]bool{},
		keys:     map[string]map[string]bool{},
	}
	if err := p.parseLines(data); err != nil {
		return nil, err
	}
	return p.doc, nil
}

// firstOversizedLine names the first line over the per-line limit, so an
// oversized single line reports its line rather than the whole file.
func firstOversizedLine(data []byte) int {
	line := 1
	start := 0
	for start <= len(data) {
		end := indexByte(data[start:], '\n')
		raw := data[start:]
		if end >= 0 {
			raw = data[start : start+end]
		}
		if len(raw) > maxLineBytes {
			return line
		}
		if end < 0 {
			break
		}
		line++
		start += end + 1
	}
	return 0
}

type parser struct {
	path     string
	doc      *tomlDoc
	explicit map[string]bool
	keys     map[string]map[string]bool
	current  *tomlTable
}

func (p *parser) parseLines(data []byte) error {
	lineNo := 0
	start := 0
	for start <= len(data) {
		end := indexByte(data[start:], '\n')
		raw := data[start:]
		if end >= 0 {
			raw = data[start : start+end]
		}
		lineNo++
		if len(raw) > maxLineBytes {
			return &Error{Path: p.path, Line: lineNo, Message: "line is longer than 1 MiB"}
		}
		line := string(raw)
		line = strings.TrimSuffix(line, "\r")
		if err := p.parseLine(line, lineNo); err != nil {
			return err
		}
		if end < 0 {
			break
		}
		start += end + 1
	}
	return nil
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func (p *parser) parseLine(line string, lineNo int) error {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || trimmed[0] == '#' {
		return nil
	}
	if trimmed[0] == '[' {
		return p.parseTableHeader(trimmed, lineNo)
	}
	return p.parseKeyValue(trimmed, lineNo)
}

func (p *parser) parseTableHeader(line string, lineNo int) error {
	if strings.HasPrefix(line, "[[") {
		return &Error{Path: p.path, Line: lineNo, Message: "arrays of tables are not supported"}
	}
	close := strings.IndexByte(line, ']')
	if close < 0 {
		return &Error{Path: p.path, Line: lineNo, Message: "unterminated table header"}
	}
	name := strings.TrimSpace(line[1:close])
	if rest := strings.TrimSpace(line[close+1:]); rest != "" && !strings.HasPrefix(rest, "#") {
		return &Error{Path: p.path, Line: lineNo, Message: "unexpected text after table header"}
	}
	if name == "" {
		return &Error{Path: p.path, Line: lineNo, Message: "expected a table name"}
	}
	segments := strings.Split(name, ".")
	for i, segment := range segments {
		segments[i] = strings.TrimSpace(segment)
		if segments[i] == "" {
			return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf("invalid table name [%s]", name)}
		}
		if segments[i][0] == '"' || segments[i][0] == '\'' {
			return &Error{Path: p.path, Line: lineNo, Message: "quoted keys are not supported"}
		}
		if err := validateBareKey(segments[i], p.path, lineNo); err != nil {
			return err
		}
	}
	full := strings.Join(segments, ".")
	lower := strings.ToLower(full)
	if strings.HasPrefix(lower, "color.theme.") {
		role := segments[2]
		return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf(
			"[Color.theme.%s] is not supported; write %s as an inline table in [Color.theme]", role, role)}
	}
	name = canonicalTable(full)
	if p.explicit[name] {
		return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf("duplicate table [%s]", name)}
	}
	p.explicit[name] = true

	tbl := tomlTable{name: name, line: lineNo}
	p.doc.tables = append(p.doc.tables, tbl)
	p.current = &p.doc.tables[len(p.doc.tables)-1]
	// [Color.theme] implicitly creates [Color]; a later explicit [Color] is
	// allowed once (FR-052), which the explicit-name check above already
	// permits because the implicit creation is not recorded as explicit.
	return nil
}

func (p *parser) parseKeyValue(line string, lineNo int) error {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return &Error{Path: p.path, Line: lineNo, Message: "expected key = value"}
	}
	keyPart := strings.TrimSpace(line[:eq])
	if keyPart == "" {
		return &Error{Path: p.path, Line: lineNo, Message: "expected a key before '='"}
	}
	if keyPart[0] == '"' || keyPart[0] == '\'' {
		return &Error{Path: p.path, Line: lineNo, Message: "quoted keys are not supported"}
	}
	if strings.Contains(keyPart, ".") {
		return &Error{Path: p.path, Line: lineNo, Message: "dotted keys are not supported"}
	}
	if err := validateBareKey(keyPart, p.path, lineNo); err != nil {
		return err
	}
	if p.current != nil && isLegacyKey(keyPart) && !canonicalKeyInTable(p.current.name, keyPart) {
		return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf(
			"unknown key %q; rev-5 keys must appear before the first table", keyPart)}
	}
	rest := line[eq+1:]
	if strings.TrimSpace(rest) == "" {
		return &Error{Path: p.path, Line: lineNo, Message: "expected a value after '='"}
	}

	bareOK := p.current == nil && isLegacyKey(keyPart)
	vp := &valueParser{path: p.path, line: lineNo, input: rest, allowBare: bareOK}
	value, err := vp.parseValue()
	if err != nil {
		return err
	}
	vp.skipSpace()
	if vp.pos < len(vp.input) {
		if vp.input[vp.pos] == '#' {
			vp.pos = len(vp.input)
		} else {
			return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf("unexpected text after value: %q", strings.TrimSpace(vp.input[vp.pos:]))}
		}
	}

	scope := ""
	var entries *[]tomlEntry
	if p.current == nil {
		entries = &p.doc.top
	} else {
		scope = p.current.name
		entries = &p.current.entries
	}
	if p.keys[scope] == nil {
		p.keys[scope] = map[string]bool{}
	}
	if p.keys[scope][keyPart] {
		return &Error{Path: p.path, Line: lineNo, Message: fmt.Sprintf("duplicate key %q", keyPart)}
	}
	p.keys[scope][keyPart] = true
	*entries = append(*entries, tomlEntry{key: keyPart, line: lineNo, value: value})
	return nil
}

// canonicalTable maps the lowercase aliases of FR-051 onto the canonical
// table names. Unknown names pass through so validation can reject them.
func canonicalTable(name string) string {
	switch name {
	case "nala", "Nala":
		return "Nala"
	case "ui", "Ui":
		return "Ui"
	case "color", "Color":
		return "Color"
	case "color.theme", "Color.theme":
		return "Color.theme"
	case "xbps", "Xbps":
		return "Xbps"
	}
	return name
}

// validateBareKey enforces TOML's bare-key alphabet and rejects the non-ASCII
// keys FR-052 names explicitly.
func validateBareKey(key, path string, line int) error {
	for _, r := range key {
		switch {
		case r > 127:
			return &Error{Path: path, Line: line, Message: "non-ASCII bare keys are not supported"}
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return &Error{Path: path, Line: line, Message: fmt.Sprintf("invalid bare key %q", key)}
		}
	}
	return nil
}

type valueParser struct {
	path      string
	line      int
	input     string
	pos       int
	allowBare bool
}

func (p *valueParser) errorf(format string, args ...any) error {
	return &Error{Path: p.path, Line: p.line, Message: fmt.Sprintf(format, args...)}
}

func (p *valueParser) skipSpace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

func (p *valueParser) parseValue() (tomlValue, error) {
	p.skipSpace()
	if p.pos >= len(p.input) || p.input[p.pos] == '#' {
		return tomlValue{}, p.errorf("expected a value after '='")
	}
	switch c := p.input[p.pos]; {
	case c == '"' || c == '\'':
		return p.parseString()
	case c == '[':
		return p.parseArray()
	case c == '{':
		return p.parseInlineTable()
	case strings.HasPrefix(p.input[p.pos:], "true"):
		return p.parseKeyword("true", true)
	case strings.HasPrefix(p.input[p.pos:], "false"):
		return p.parseKeyword("false", false)
	default:
		return p.parseScalar()
	}
}

func (p *valueParser) parseKeyword(word string, value bool) (tomlValue, error) {
	// `truex` is a bare value, not the boolean true followed by text.
	end := p.pos + len(word)
	if end < len(p.input) && !isValueDelimiter(p.input[end]) {
		return p.parseScalar()
	}
	p.pos = end
	return tomlValue{kind: tomlBool, boolean: value, line: p.line}, nil
}

func isValueDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', ',', ']', '}', '#':
		return true
	}
	return false
}

func (p *valueParser) parseString() (tomlValue, error) {
	quote := p.input[p.pos]
	if p.pos+2 < len(p.input) && p.input[p.pos+1] == quote && p.input[p.pos+2] == quote {
		return tomlValue{}, p.errorf("multiline strings are not supported")
	}
	p.pos++
	var b strings.Builder
	if quote == '\'' {
		for {
			if p.pos >= len(p.input) {
				return tomlValue{}, p.errorf("unterminated string")
			}
			c := p.input[p.pos]
			p.pos++
			if c == '\'' {
				return tomlValue{kind: tomlString, str: b.String(), line: p.line}, nil
			}
			b.WriteByte(c)
		}
	}
	for {
		if p.pos >= len(p.input) {
			return tomlValue{}, p.errorf("unterminated string")
		}
		c := p.input[p.pos]
		p.pos++
		if c == '"' {
			return tomlValue{kind: tomlString, str: b.String(), line: p.line}, nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if p.pos >= len(p.input) {
			return tomlValue{}, p.errorf("unterminated string")
		}
		esc := p.input[p.pos]
		p.pos++
		switch esc {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u':
			r, err := p.readUnicodeEscape(4)
			if err != nil {
				return tomlValue{}, err
			}
			b.WriteRune(r)
		case 'U':
			r, err := p.readUnicodeEscape(8)
			if err != nil {
				return tomlValue{}, err
			}
			b.WriteRune(r)
		default:
			return tomlValue{}, p.errorf("invalid escape sequence \\%c", esc)
		}
	}
}

func (p *valueParser) readUnicodeEscape(digits int) (rune, error) {
	if p.pos+digits > len(p.input) {
		return 0, p.errorf("invalid unicode escape")
	}
	hex := p.input[p.pos : p.pos+digits]
	var value int64
	for _, c := range hex {
		switch {
		case c >= '0' && c <= '9':
			value = value*16 + int64(c-'0')
		case c >= 'a' && c <= 'f':
			value = value*16 + int64(c-'a'+10)
		case c >= 'A' && c <= 'F':
			value = value*16 + int64(c-'A'+10)
		default:
			return 0, p.errorf("invalid unicode escape \\%s", hex)
		}
	}
	p.pos += digits
	if value > utf8.MaxRune || (value >= 0xd800 && value <= 0xdfff) {
		return 0, p.errorf("invalid unicode escape \\%s", hex)
	}
	return rune(value), nil
}

func (p *valueParser) parseArray() (tomlValue, error) {
	p.pos++ // '['
	value := tomlValue{kind: tomlArray, line: p.line}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] == '#' {
			return tomlValue{}, p.errorf("arrays must be on one line")
		}
		if p.input[p.pos] == ']' {
			p.pos++
			return value, nil
		}
		element, err := p.parseValue()
		if err != nil {
			return tomlValue{}, err
		}
		value.array = append(value.array, element)
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] == '#' {
			return tomlValue{}, p.errorf("arrays must be on one line")
		}
		switch p.input[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return value, nil
		default:
			return tomlValue{}, p.errorf("expected ',' or ']' in array")
		}
	}
}

func (p *valueParser) parseInlineTable() (tomlValue, error) {
	p.pos++ // '{'
	value := tomlValue{kind: tomlTableValue, line: p.line}
	seen := map[string]bool{}
	for {
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] == '#' {
			return tomlValue{}, p.errorf("inline tables must be on one line")
		}
		if p.input[p.pos] == '}' {
			p.pos++
			return value, nil
		}
		key, err := p.parseInlineKey()
		if err != nil {
			return tomlValue{}, err
		}
		if seen[key] {
			return tomlValue{}, p.errorf("duplicate key %q", key)
		}
		seen[key] = true
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] != '=' {
			return tomlValue{}, p.errorf("expected '=' after key %q", key)
		}
		p.pos++
		entry, err := p.parseValue()
		if err != nil {
			return tomlValue{}, err
		}
		value.table = append(value.table, tomlEntry{key: key, line: p.line, value: entry})
		p.skipSpace()
		if p.pos >= len(p.input) || p.input[p.pos] == '#' {
			return tomlValue{}, p.errorf("inline tables must be on one line")
		}
		switch p.input[p.pos] {
		case ',':
			p.pos++
			p.skipSpace()
			if p.pos < len(p.input) && p.input[p.pos] == '}' {
				return tomlValue{}, p.errorf("inline tables must not have a trailing comma")
			}
		case '}':
			p.pos++
			return value, nil
		default:
			return tomlValue{}, p.errorf("expected ',' or '}' in inline table")
		}
	}
}

func (p *valueParser) parseInlineKey() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return "", p.errorf("expected a key in inline table")
	}
	start := p.pos
	if p.input[p.pos] == '"' || p.input[p.pos] == '\'' {
		return "", p.errorf("quoted keys are not supported")
	}
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if c == ' ' || c == '\t' || c == '=' || c == ',' || c == '}' {
			break
		}
		if c == '.' {
			return "", p.errorf("dotted keys are not supported")
		}
		p.pos++
	}
	key := p.input[start:p.pos]
	if key == "" {
		return "", p.errorf("expected a key in inline table")
	}
	if err := validateBareKey(key, p.path, p.line); err != nil {
		return "", err
	}
	return key, nil
}

// parseScalar handles integers, bare values and rejects every value form the
// subset deliberately does not implement.
func (p *valueParser) parseScalar() (tomlValue, error) {
	start := p.pos
	for p.pos < len(p.input) && !isValueDelimiter(p.input[p.pos]) {
		p.pos++
	}
	token := p.input[start:p.pos]
	if token == "" {
		return tomlValue{}, p.errorf("expected a value after '='")
	}

	unsigned := token
	if strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
		unsigned = unsigned[1:]
	}
	lower := strings.ToLower(unsigned)
	if strings.HasPrefix(lower, "0x") || strings.HasPrefix(lower, "0o") || strings.HasPrefix(lower, "0b") {
		return tomlValue{}, p.errorf("hexadecimal, octal and binary integers are not supported")
	}
	lowerToken := strings.ToLower(token)
	if lowerToken == "inf" || lowerToken == "+inf" || lowerToken == "-inf" ||
		lowerToken == "nan" || lowerToken == "+nan" || lowerToken == "-nan" {
		return tomlValue{}, p.errorf("floats are not supported")
	}
	if startsNumeric(token) {
		if strings.Contains(token, "_") {
			return tomlValue{}, p.errorf("underscores in integers are not supported")
		}
		if looksLikeDateOrTime(token) {
			return tomlValue{}, p.errorf("dates and times are not supported")
		}
		if looksLikeFloat(token) {
			return tomlValue{}, p.errorf("floats are not supported")
		}
	}

	if isDecimalInteger(token) {
		if len(unsigned) > 1 && unsigned[0] == '0' {
			return tomlValue{}, p.errorf("leading zeros in integers are not supported")
		}
		value, err := parseInt64(token)
		if err != nil {
			return tomlValue{}, p.errorf("integer %q is out of range", token)
		}
		return tomlValue{kind: tomlInteger, integer: value, line: p.line}, nil
	}
	if isBareToken(token) {
		if !p.allowBare {
			return tomlValue{}, p.errorf("bare values are not supported; strings must be quoted")
		}
		return tomlValue{kind: tomlBare, str: token, line: p.line}, nil
	}
	return tomlValue{}, p.errorf("invalid value %q", token)
}

func isBareToken(token string) bool {
	for _, r := range token {
		if r > 127 {
			return false
		}
	}
	return true
}

func isDecimalInteger(token string) bool {
	i := 0
	if len(token) > 0 && (token[0] == '+' || token[0] == '-') {
		i = 1
	}
	if i >= len(token) {
		return false
	}
	for ; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return false
		}
	}
	return true
}

func parseInt64(token string) (int64, error) {
	negative := false
	if len(token) > 0 && (token[0] == '+' || token[0] == '-') {
		negative = token[0] == '-'
		token = token[1:]
	}
	var value int64
	for _, c := range token {
		digit := int64(c - '0')
		if value > (1<<63-1-digit)/10 {
			return 0, fmt.Errorf("out of range")
		}
		value = value*10 + digit
	}
	if negative {
		value = -value
	}
	return value, nil
}

func looksLikeDateOrTime(token string) bool {
	if len(token) >= 10 && token[4] == '-' && token[7] == '-' {
		return true
	}
	if len(token) >= 8 && token[2] == ':' && token[5] == ':' {
		return true
	}
	return false
}

func startsNumeric(token string) bool {
	if token == "" {
		return false
	}
	switch c := token[0]; {
	case c >= '0' && c <= '9', c == '+', c == '-', c == '.':
		return true
	}
	return false
}

func looksLikeFloat(token string) bool {
	lower := strings.ToLower(token)
	if lower == "inf" || lower == "+inf" || lower == "-inf" || lower == "nan" || lower == "+nan" || lower == "-nan" {
		return true
	}
	return strings.ContainsAny(token, ".eE")
}
