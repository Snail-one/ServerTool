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
$version = $env:VERSION
if ([string]::IsNullOrWhiteSpace($version)) {
	$version = git describe --tags --always --dirty 2>$null
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($version)) {
		$version = 'dev'
	}
}

$commit = $env:COMMIT
if ([string]::IsNullOrWhiteSpace($commit)) {
	$commit = git rev-parse --short HEAD 2>$null
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) {
		$commit = 'unknown'
	}
}

$buildDate = $env:BUILD_DATE
if ([string]::IsNullOrWhiteSpace($buildDate)) {
	$buildDate = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
}

$ldflags = "-s -w -X snail_tool/internal/version.Version=$version -X snail_tool/internal/version.Commit=$commit -X snail_tool/internal/version.BuildDate=$buildDate"
go build -ldflags $ldflags -o $output ./cmd/snail_tool

Write-Host "Build completed: $output"
