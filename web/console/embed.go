package consoleweb

import _ "embed"

//go:embed app.css
var CSS []byte

//go:embed vendor/datastar-v1.0.2.js
var Datastar []byte

//go:embed vendor/datastar-v1.0.2.LICENSE
var DatastarLicense []byte
