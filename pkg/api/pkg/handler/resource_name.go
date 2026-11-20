package handler

import "regexp"

var resourceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
