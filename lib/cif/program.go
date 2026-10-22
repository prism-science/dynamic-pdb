package cif

import "strings"

func mmcifProgram(document *Document) Program {
	if software := document.Table("_software"); software != nil {
		var chosen []string
		for _, row := range software.Rows {
			if strings.Contains(strings.ToLower(software.Value(row, "classification")), "refinement") {
				chosen = row
				break
			}
		}
		if chosen == nil && len(software.Rows) > 0 {
			chosen = software.Rows[len(software.Rows)-1]
		}
		if chosen != nil {
			if name := software.Value(chosen, "name"); name != "" {
				return Program{Name: name, Version: cleanVersion(software.Value(chosen, "version"))}
			}
		}
	}
	if computing, ok := document.Scalar("_computing.structure_refinement"); ok {
		return splitProgram(computing)
	}
	return Program{}
}

func splitProgram(raw string) Program {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Program{}
	}
	if open := strings.LastIndex(raw, "("); open > 0 && strings.HasSuffix(raw, ")") {
		return Program{
			Name:    strings.TrimSpace(raw[:open]),
			Version: cleanVersion(strings.TrimSuffix(raw[open+1:], ")")),
		}
	}
	fields := strings.Fields(raw)
	if len(fields) > 1 && startsWithDigit(strings.TrimPrefix(fields[len(fields)-1], "v")) {
		return Program{
			Name:    strings.Join(fields[:len(fields)-1], " "),
			Version: cleanVersion(strings.TrimPrefix(fields[len(fields)-1], "v")),
		}
	}
	return Program{Name: raw}
}

func cleanVersion(raw string) string {
	parts := strings.Split(raw, ":")
	version := strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		right := strings.TrimSpace(parts[len(parts)-1])
		if startsWithDigit(strings.TrimPrefix(right, "v")) {
			version = right
		}
	}
	if version == "" || strings.Trim(version, "?") == "" {
		return ""
	}
	return version
}

func startsWithDigit(value string) bool {
	return value != "" && value[0] >= '0' && value[0] <= '9'
}
