param(
	[string]$GoArch = 'amd64'
)

$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root

$outDir = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$env:CGO_ENABLED = '0'
$env:GOOS = 'linux'
$env:GOARCH = $GoArch

$version = $env:VERSION
if ([string]::IsNullOrWhiteSpace($version)) {
	$version = git describe --tags --always --dirty 2>$null
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($version)) {
		$version = 'dev'
	}
}
if ($version -notmatch '^[A-Za-z0-9._-]+$') {
	throw "Unsupported version for artifact filename: $version"
}
$output = Join-Path $outDir "snailtool_linux_${GoArch}_${version}"

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
