@echo off
rem SPDX-License-Identifier: Apache-2.0
rem Command-line entry point for the Derive World local launcher.
setlocal
powershell.exe -NoProfile -File "%~dp0DeriveWorld.ps1" %*
exit /b %errorlevel%
