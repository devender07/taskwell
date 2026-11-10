package web

import (
	"encoding/csv"
	"net/http"
	"strings"
)

// WriteCSV answers a download named filename: the header, then rows. A cell starting with = + - or @ gets a leading
// ' so a spreadsheet shows it instead of running it as a formula.
func WriteCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	cw := csv.NewWriter(w)
	_ = cw.Write(header)
	for _, row := range rows {
		for i, v := range row {
			if v != "" && strings.ContainsRune("=+-@", rune(v[0])) {
				row[i] = "'" + v
			}
		}
		_ = cw.Write(row) // a write error is the client gone; nothing to answer
	}
	cw.Flush()
}
