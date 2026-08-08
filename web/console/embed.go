package consoleweb

import _ "embed"

//go:embed index.html
var Index []byte

//go:embed app.css
var CSS []byte

//go:embed app.js
var JavaScript []byte
