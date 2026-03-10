package util

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

func ExtractAllTags(xmlStr, tagName string) []string {
	var results []string
	decoder := xml.NewDecoder(strings.NewReader(xmlStr))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch se := tok.(type) {
		case xml.StartElement:
			if se.Name.Local == tagName {
				var buf strings.Builder
				xml.NewEncoder(&buf).EncodeToken(se)
				results = append(results, buf.String())
			}
		}
	}
	return results
}

// ExtractAttr finds the first occurrence.
func ExtractAttr(xml, attr string) string {
	idx := strings.Index(xml, attr+"=\"")
	quoteChar := "\""

	if idx == -1 {
		idx = strings.Index(xml, attr+"='")
		quoteChar = "'"
	}

	if idx == -1 {
		return ""
	}

	start := idx + len(attr) + 2
	end := strings.Index(xml[start:], quoteChar)
	if end == -1 {
		return ""
	}

	return xml[start : start+end]
}

// ExtractAttrInTag finds attr only in a specific tag.
func ExtractAttrInTag(xml, tagName, attr string) string {
	tagStart := strings.Index(xml, "<"+tagName)
	if tagStart == -1 {
		return ""
	}

	tagEnd := strings.Index(xml[tagStart:], ">")
	if tagEnd == -1 {
		return ""
	}

	tagContent := xml[tagStart : tagStart+tagEnd]
	return ExtractAttr(tagContent, attr)
}

// ExtractMultipleTags returns all matches for <tagName>...</tagName> or self-closing <tagName ... />.
func ExtractMultipleTags(xml, tag string) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?s)<%s(?:[^>]*/>|[^>]*>.*?</%s>)`, tag, tag))
	return re.FindAllString(xml, -1)
}

// ExtractTagValue extracts the text inside a given XML tag.
// Example: <jid>foo@bar</jid> => "foo@bar"
func ExtractTagValue(xml, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`<%s[^>]*>([^<]*)</%s>`, tag, tag))
	matches := re.FindStringSubmatch(xml)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}
