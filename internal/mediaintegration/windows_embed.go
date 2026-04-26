//go:build windows && saki_embed_smtc

package mediaintegration

import _ "embed"

//go:embed smtc_shim/windows/saki_smtc.dll
var embeddedSMTCDLL []byte
