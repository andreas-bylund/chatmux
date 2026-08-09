package twitch

import "strings"

// ircLine is a parsed IRCv3 message: optional @tags, optional :prefix, a
// command, and its parameters (the trailing ":..." param is the last element).
type ircLine struct {
	tags   map[string]string
	prefix string
	cmd    string
	params []string
}

// param returns the i-th parameter, or "" if there aren't that many.
func (l ircLine) param(i int) string {
	if i < 0 || i >= len(l.params) {
		return ""
	}
	return l.params[i]
}

// nick pulls the nickname out of a "nick!user@host" prefix.
func (l ircLine) nick() string {
	if i := strings.IndexByte(l.prefix, '!'); i >= 0 {
		return l.prefix[:i]
	}
	return l.prefix
}

func parseIRC(line string) (ircLine, bool) {
	line = strings.TrimRight(line, "\r\n")
	var l ircLine

	if strings.HasPrefix(line, "@") {
		raw, rest, ok := strings.Cut(line[1:], " ")
		if !ok {
			return l, false
		}
		l.tags = parseTags(raw)
		line = rest
	}

	if strings.HasPrefix(line, ":") {
		prefix, rest, ok := strings.Cut(line[1:], " ")
		if !ok {
			return l, false
		}
		l.prefix = prefix
		line = rest
	}

	line = strings.TrimLeft(line, " ")
	if line == "" {
		return l, false
	}

	cmd, rest, _ := strings.Cut(line, " ")
	l.cmd = strings.ToUpper(cmd)

	for rest != "" {
		rest = strings.TrimLeft(rest, " ")
		if rest == "" {
			break
		}
		if rest[0] == ':' {
			l.params = append(l.params, rest[1:])
			break
		}
		p, r, ok := strings.Cut(rest, " ")
		l.params = append(l.params, p)
		if !ok {
			break
		}
		rest = r
	}
	return l, true
}

func parseTags(raw string) map[string]string {
	tags := make(map[string]string)
	for _, kv := range strings.Split(raw, ";") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		tags[k] = unescapeTag(v)
	}
	return tags
}

// unescapeTag reverses the IRCv3 tag escaping (\s for space, \: for semicolon,
// and so on). Display names with spaces arrive escaped.
func unescapeTag(v string) string {
	if !strings.ContainsRune(v, '\\') {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i == len(v)-1 {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case ':':
			b.WriteByte(';')
		case 's':
			b.WriteByte(' ')
		case 'r':
			b.WriteByte('\r')
		case 'n':
			b.WriteByte('\n')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(v[i])
		}
	}
	return b.String()
}
