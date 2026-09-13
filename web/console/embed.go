package consoleweb

import _ "embed"

//go:embed app.css
var baseCSS []byte

//go:embed graph_workspace.css
var graphWorkspaceCSS []byte

var CSS = append(append([]byte{}, baseCSS...), graphWorkspaceCSS...)

// NavigationJS progressively restores collection viewport and focus. Native
// links remain complete when scripts or session storage are unavailable.
//
//go:embed navigation.js
var navigationJS []byte

//go:embed graph_workspace.js
var graphWorkspaceJS []byte

var NavigationJS = append(append([]byte{}, navigationJS...), graphWorkspaceJS...)

//go:embed vendor/datastar-v1.0.2.js
var Datastar []byte

//go:embed vendor/datastar-v1.0.2.LICENSE
var DatastarLicense []byte
