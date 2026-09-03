package ui

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFSRaw embed.FS

// StaticFS is the embedded static asset tree, rooted so it serves at /static/.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticFSRaw, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

// Each page is parsed as its own template set (layout + that page's content,
// plus logs_results.html where a page embeds the results fragment) so that
// the "content" block name can be reused per-page without collisions.
var (
	indexTmpl        = mustParse("layout.html", "index.html")
	logsTmpl         = mustParse("layout.html", "logs.html", "logs_results.html")
	resultsTmpl      = mustParse("logs_results.html")
	traceTmpl        = mustParse("layout.html", "trace.html")
	queryTmpl        = mustParse("layout.html", "query.html", "query_results.html")
	queryResultsTmpl = mustParse("query_results.html")
)

func mustParse(files ...string) *template.Template {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = "templates/" + f
	}
	return template.Must(template.ParseFS(templatesFS, paths...))
}
