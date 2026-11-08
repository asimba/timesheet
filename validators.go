package main

import "regexp"

var nameRe = regexp.MustCompile(`^[\p{L}\s-]+$`)

func isValidName(s string) bool {
	if s == "" {
		return false
	}
	if !nameRe.MatchString(s) {
		return false
	}
	trimmed := s
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '-') {
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '-') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed != ""
}