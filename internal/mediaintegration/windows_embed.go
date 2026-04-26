//go:build windows

package mediaintegration

import _ "embed"

//go:embed smtc_shim/windows/saki_smtc.dll
var embeddedSMTCDLL []byte
