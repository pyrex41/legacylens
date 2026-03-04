package rag

import (
	"fmt"
	"strings"
)

func BuildFrontmatter(c Chunk) string {
	var b strings.Builder
	b.WriteString("---\n")
	if c.Name != "" {
		b.WriteString(fmt.Sprintf("title: %s\n", safeYAMLValue(c.Name)))
	} else {
		b.WriteString("title: unnamed\n")
	}
	if c.Description != "" {
		b.WriteString(fmt.Sprintf("description: %s\n", safeYAMLValue(c.Description)))
	} else {
		b.WriteString(fmt.Sprintf("description: %s\n", safeYAMLValue("Fortran semantic unit")))
	}
	b.WriteString("parameters:\n")
	if len(c.Parameters) == 0 {
		b.WriteString("  []\n")
	} else {
		for _, p := range c.Parameters {
			b.WriteString("  - name: " + safeYAMLValue(p.Name) + "\n")
			b.WriteString("    type: " + safeYAMLValue(zeroDefault(p.Type, "unknown")) + "\n")
			b.WriteString("    intent: " + safeYAMLValue(zeroDefault(p.Intent, "unknown")) + "\n")
			b.WriteString("    description: " + safeYAMLValue(zeroDefault(p.Description, "n/a")) + "\n")
		}
	}
	b.WriteString("skills:\n")
	if len(c.Skills) == 0 {
		b.WriteString("  - " + safeYAMLValue("fortran") + "\n")
		b.WriteString("  - " + safeYAMLValue("legacy-code") + "\n")
	} else {
		for _, s := range c.Skills {
			b.WriteString("  - " + safeYAMLValue(s) + "\n")
		}
	}
	b.WriteString("---")
	return b.String()
}

func safeYAMLValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	escaped := strings.ReplaceAll(v, "\"", "\\\"")
	return fmt.Sprintf("\"%s\"", escaped)
}

func zeroDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
