package consoleweb

import _ "embed"

//go:embed app.css
var baseCSS []byte

//go:embed graph_workspace.css
var graphWorkspaceCSS []byte

//go:embed loop_workspace.css
var loopWorkspaceCSS []byte

var CSS = append(append(append([]byte{}, baseCSS...), graphWorkspaceCSS...), loopWorkspaceCSS...)

// NavigationJS progressively restores collection viewport and focus. Native
// links remain complete when scripts or session storage are unavailable.
//
//go:embed navigation.js
var navigationJS []byte

//go:embed graph_workspace.js
var graphWorkspaceJS []byte

//go:embed loop_workspace.js
var loopWorkspaceJS []byte

var NavigationJS = append(append(append([]byte{}, navigationJS...), graphWorkspaceJS...), loopWorkspaceJS...)

//go:embed vendor/datastar-v1.0.2.js
var Datastar []byte

//go:embed vendor/datastar-v1.0.2.LICENSE
var DatastarLicense []byte
