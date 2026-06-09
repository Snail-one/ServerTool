param(
	[string]$GoArch = 'amd64'
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

$outDir = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
$env:GOARCH = $GoArch

$output = Join-Path $outDir "snail_tool_linux_$GoArch"
go build -o $output ./cmd/snail_tool

Write-Host "Build completed: $output"
