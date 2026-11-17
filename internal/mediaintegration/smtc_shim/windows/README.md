# Windows SMTC Shim

This DLL bridges Go to Windows Runtime `SystemMediaTransportControls`.

Build from a Developer PowerShell with MSVC and a Windows SDK:

```pwsh
cl /std:c++17 /EHsc /DUNICODE /D_UNICODE /D_SILENCE_EXPERIMENTAL_COROUTINE_DEPRECATION_WARNINGS /LD saki_smtc.cpp /link windowsapp.lib /OUT:saki_smtc.dll
```

Windows Go builds embed the resulting `saki_smtc.dll` into `saki.exe`. At
runtime, Saki extracts it to `%USERPROFILE%\.cache\saki\smtc\saki_smtc.dll`
(below `%XDG_CACHE_HOME%` when that is an absolute path) and loads that cached
copy. Releases up to v1.0.0 extracted it to `%LOCALAPPDATA%\Saki\smtc`, which
`saki migrate` deletes. Keeping a DLL next to `saki.exe` or on `PATH` is only a
fallback for development or failed extraction.
