package consoleweb

import _ "embed"

//go:embed app.css
var CSS []byte

// NavigationJS progressively restores collection viewport and focus. Native
// links remain complete when scripts or session storage are unavailable.
//
//go:embed navigation.js
var NavigationJS []byte

//go:embed vendor/datastar-v1.0.2.js
var Datastar []byte

//go:embed vendor/datastar-v1.0.2.LICENSE
var DatastarLicense []byte
