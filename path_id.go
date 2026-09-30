package main

import (
	"net/http"
	"strconv"
)

func parsePathID(w http.ResponseWriter, raw string) (int, bool) {
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid ID: expected positive integer")
		return 0, false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			writeError(w, http.StatusBadRequest, "invalid ID: expected positive integer")
			return 0, false
		}
	}
	return id, true
}
