package licenses

import _ "embed"

// ThirdParty contains the license and attribution notices for software linked
// into the released binaries or bundled into the embedded web application.
//
//go:embed THIRD_PARTY_LICENSES.txt
var ThirdParty string
