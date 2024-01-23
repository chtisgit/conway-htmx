package main

type FieldData struct {
	Rows, Cols int
	Cells      [][]byte
}

const templatesSource = `
{{ define "cell-id" }}cell{{ .X }}-{{ .Y }}{{end}}

{{ define "cell" }}
<form{{ if ne .Val 0 }} class="black" {{end}} hx-post="game/cell" hx-trigger="click" hx-swap="outerHTML" sse-swap="{{ template "cell-id" . }}">
	<input type="hidden" name="x" value="{{ .X }}">
	<input type="hidden" name="y" value="{{ .Y }}">
</form>
{{ end }}

display: grid;

{{ define "field" }}
<div class="game-grid" style="grid-template-columns: repeat({{ .Cols }}, 1fr);grid-template-rows: repeat({{ .Rows }}, 1fr)">
{{ range $y, $row := .Cells }}
	{{ range $x, $val := $row }}
		{{ template "cell" (makeCell $x $y $val)}}
	{{ end }}
{{ end }}
</div>
{{ end }}
`
