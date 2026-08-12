package dst

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type dstLuaToken struct {
	kind       byte
	text       string
	start, end int
}

type dstLuaValue struct {
	kind       byte
	text       string
	start, end int
	table      *dstLuaTable
}

type dstLuaEntry struct {
	key   string
	value dstLuaValue
}

type dstLuaTable struct {
	entries     map[string]dstLuaEntry
	open, close int
}

type dstWorldgenDocument struct {
	root      *dstLuaTable
	overrides *dstLuaTable
	preset    string
}

type dstLuaParser struct {
	source string
	tokens []dstLuaToken
	index  int
}

func parseDSTWorldgen(source string) (dstWorldgenDocument, error) {
	tokens, err := lexDSTStaticLua(source)
	if err != nil {
		return dstWorldgenDocument{}, err
	}
	parser := dstLuaParser{source: source, tokens: tokens}
	if token := parser.next(); token.kind != 'i' || token.text != "return" {
		return dstWorldgenDocument{}, fmt.Errorf("文件必须以 return { 开始")
	}
	value, err := parser.parseValue()
	if err != nil {
		return dstWorldgenDocument{}, err
	}
	if value.kind != 't' || value.table == nil {
		return dstWorldgenDocument{}, fmt.Errorf("return 后必须是静态 table")
	}
	if token := parser.next(); token.kind != 0 {
		return dstWorldgenDocument{}, fmt.Errorf("静态 table 后存在不支持的 Lua 语句")
	}
	document := dstWorldgenDocument{root: value.table}
	if entry, ok := value.table.entries["override_enabled"]; ok {
		if entry.value.kind != 'b' || entry.value.text != "true" {
			return dstWorldgenDocument{}, fmt.Errorf("override_enabled 必须为 true")
		}
	}
	if entry, ok := value.table.entries["preset"]; ok {
		if entry.value.kind != 's' {
			return dstWorldgenDocument{}, fmt.Errorf("preset 必须是字符串")
		}
		document.preset = entry.value.text
	}
	if entry, ok := value.table.entries["overrides"]; ok {
		if entry.value.kind != 't' || entry.value.table == nil {
			return dstWorldgenDocument{}, fmt.Errorf("overrides 必须是静态 table")
		}
		document.overrides = entry.value.table
		for key, override := range document.overrides.entries {
			if override.value.kind != 's' && override.value.kind != 'b' && override.value.kind != 'n' {
				return dstWorldgenDocument{}, fmt.Errorf("overrides.%s 只能使用字符串、布尔值或数字", key)
			}
		}
	}
	return document, nil
}

func (p *dstLuaParser) parseValue() (dstLuaValue, error) {
	token := p.next()
	switch token.kind {
	case 's', 'n':
		return dstLuaValue{kind: token.kind, text: token.text, start: token.start, end: token.end}, nil
	case 'i':
		if token.text == "true" || token.text == "false" {
			return dstLuaValue{kind: 'b', text: token.text, start: token.start, end: token.end}, nil
		}
		if token.text == "nil" {
			return dstLuaValue{kind: '0', text: token.text, start: token.start, end: token.end}, nil
		}
		return dstLuaValue{}, fmt.Errorf("不支持动态 Lua 值 %q", token.text)
	case '{':
		table := &dstLuaTable{entries: make(map[string]dstLuaEntry), open: token.start}
		for {
			if p.peek().kind == '}' {
				table.close = p.next().start
				return dstLuaValue{kind: 't', start: token.start, end: table.close + 1, table: table}, nil
			}
			keyToken := p.next()
			key := ""
			switch keyToken.kind {
			case 'i':
				key = keyToken.text
			case '[':
				stringToken := p.next()
				if stringToken.kind != 's' || p.next().kind != ']' {
					return dstLuaValue{}, fmt.Errorf("table 的方括号键只支持字符串")
				}
				key = stringToken.text
			default:
				return dstLuaValue{}, fmt.Errorf("table 中存在不支持的键")
			}
			if p.next().kind != '=' {
				return dstLuaValue{}, fmt.Errorf("table 键 %q 后缺少 =", key)
			}
			if _, exists := table.entries[key]; exists {
				return dstLuaValue{}, fmt.Errorf("table 键 %q 重复", key)
			}
			value, err := p.parseValue()
			if err != nil {
				return dstLuaValue{}, err
			}
			table.entries[key] = dstLuaEntry{key: key, value: value}
			separator := p.peek().kind
			if separator == ',' || separator == ';' {
				p.next()
			} else if separator != '}' {
				return dstLuaValue{}, fmt.Errorf("table 键 %q 后缺少逗号", key)
			}
		}
	default:
		return dstLuaValue{}, fmt.Errorf("不支持的 Lua 值")
	}
}

func (p *dstLuaParser) peek() dstLuaToken {
	if p.index >= len(p.tokens) {
		return dstLuaToken{}
	}
	return p.tokens[p.index]
}

func (p *dstLuaParser) next() dstLuaToken {
	token := p.peek()
	if p.index < len(p.tokens) {
		p.index++
	}
	return token
}

func lexDSTStaticLua(source string) ([]dstLuaToken, error) {
	tokens := make([]dstLuaToken, 0, 64)
	for index := 0; index < len(source); {
		r, size := utf8.DecodeRuneInString(source[index:])
		if unicode.IsSpace(r) || r == '\uFEFF' {
			index += size
			continue
		}
		if strings.HasPrefix(source[index:], "--") {
			if strings.HasPrefix(source[index:], "--[[") {
				end := strings.Index(source[index+4:], "]]")
				if end < 0 {
					return nil, fmt.Errorf("Lua 块注释未闭合")
				}
				index += 4 + end + 2
				continue
			}
			if end := strings.IndexByte(source[index:], '\n'); end >= 0 {
				index += end + 1
			} else {
				index = len(source)
			}
			continue
		}
		start := index
		if strings.ContainsRune("{}[]=,;", r) {
			tokens = append(tokens, dstLuaToken{kind: byte(r), text: string(r), start: start, end: start + size})
			index += size
			continue
		}
		if r == '\'' || r == '"' {
			quote := byte(r)
			index += size
			var value strings.Builder
			closed := false
			for index < len(source) {
				char := source[index]
				if char == quote {
					index++
					closed = true
					break
				}
				if char == '\n' || char == '\r' {
					return nil, fmt.Errorf("Lua 字符串不能跨行")
				}
				if char == '\\' {
					if index+1 >= len(source) {
						return nil, fmt.Errorf("Lua 字符串转义未完成")
					}
					next := source[index+1]
					switch next {
					case '\\', '\'', '"':
						value.WriteByte(next)
					case 'n':
						value.WriteByte('\n')
					case 'r':
						value.WriteByte('\r')
					case 't':
						value.WriteByte('\t')
					default:
						return nil, fmt.Errorf("不支持的 Lua 字符串转义")
					}
					index += 2
					continue
				}
				value.WriteByte(char)
				index++
			}
			if !closed {
				return nil, fmt.Errorf("Lua 字符串未闭合")
			}
			tokens = append(tokens, dstLuaToken{kind: 's', text: value.String(), start: start, end: index})
			continue
		}
		if isDSTLuaIdentifierStart(r) {
			index += size
			for index < len(source) {
				next, nextSize := utf8.DecodeRuneInString(source[index:])
				if !isDSTLuaIdentifierPart(next) {
					break
				}
				index += nextSize
			}
			tokens = append(tokens, dstLuaToken{kind: 'i', text: source[start:index], start: start, end: index})
			continue
		}
		if unicode.IsDigit(r) || (r == '-' && index+1 < len(source) && source[index+1] >= '0' && source[index+1] <= '9') {
			index += size
			for index < len(source) && ((source[index] >= '0' && source[index] <= '9') || source[index] == '.') {
				index++
			}
			if _, err := strconv.ParseFloat(source[start:index], 64); err != nil {
				return nil, fmt.Errorf("无效 Lua 数字")
			}
			tokens = append(tokens, dstLuaToken{kind: 'n', text: source[start:index], start: start, end: index})
			continue
		}
		return nil, fmt.Errorf("第 %d 字节存在不支持的 Lua 语法", index+1)
	}
	tokens = append(tokens, dstLuaToken{kind: 0, start: len(source), end: len(source)})
	return tokens, nil
}

func isDSTLuaIdentifierStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isDSTLuaIdentifierPart(r rune) bool  { return isDSTLuaIdentifierStart(r) || unicode.IsDigit(r) }

func defaultDSTWorldgen(fileID string) string {
	preset := "SURVIVAL_TOGETHER"
	if fileID == "caves-world" {
		preset = "DST_CAVE"
	}
	return "return {\n    override_enabled = true,\n    preset = " + strconv.Quote(preset) + ",\n    overrides = {\n    },\n}\n"
}

func setDSTWorldgenOverride(source, key, encodedValue string) (string, error) {
	document, err := parseDSTWorldgen(source)
	if err != nil {
		return "", err
	}
	if document.overrides == nil {
		newline := dstLuaNewline(source)
		multiline := "    overrides = {" + newline + "        " + key + " = " + encodedValue + "," + newline + "    },"
		inline := "overrides = { " + key + " = " + encodedValue + ", },"
		return insertDSTStaticLuaEntry(source, document.root, multiline, inline), nil
	}
	if entry, ok := document.overrides.entries[key]; ok {
		return source[:entry.value.start] + encodedValue + source[entry.value.end:], nil
	}
	multiline := "        " + key + " = " + encodedValue + ","
	inline := key + " = " + encodedValue + ","
	return insertDSTStaticLuaEntry(source, document.overrides, multiline, inline), nil
}

func insertDSTStaticLuaEntry(source string, table *dstLuaTable, multiline, inline string) string {
	position := table.open + 1
	newline := dstLuaNewline(source)
	if strings.HasPrefix(source[position:], newline) {
		position += len(newline)
		return source[:position] + multiline + newline + source[position:]
	}
	return source[:position] + " " + inline + " " + source[position:]
}

func dstLuaNewline(source string) string {
	if strings.Contains(source, "\r\n") {
		return "\r\n"
	}
	return "\n"
}
