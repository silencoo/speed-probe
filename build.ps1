$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$ProjectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$DistDir = Join-Path $ProjectRoot "dist"
$CertDir = if ($env:SPEED_PROBE_TLS_OUTPUT_DIR) { $env:SPEED_PROBE_TLS_OUTPUT_DIR } else { Join-Path $DistDir "certs" }
$CertFile = Join-Path $CertDir "speed-probe.crt"
$KeyFile = Join-Path $CertDir "speed-probe.key"

function Assert-LastCommand([string]$Message) {
    if ($LASTEXITCODE -ne 0) {
        throw $Message
    }
}

function Test-TlsPair {
    $CertPublicKey = (& openssl x509 -in $CertFile -pubkey -noout 2>$null) -join "`n"
    Assert-LastCommand "Invalid TLS certificate: $CertFile"

    $PrivatePublicKey = (& openssl pkey -in $KeyFile -pubout 2>$null) -join "`n"
    Assert-LastCommand "Invalid TLS private key: $KeyFile"

    if ($CertPublicKey -ne $PrivatePublicKey) {
        throw "TLS certificate and private key do not match"
    }
}

function Initialize-TlsPair {
    New-Item -ItemType Directory -Force -Path $CertDir | Out-Null

    $SourceCert = $env:SPEED_PROBE_TLS_CERT_FILE
    $SourceKey = $env:SPEED_PROBE_TLS_KEY_FILE

    if ($SourceCert -or $SourceKey) {
        if (-not ($SourceCert -and $SourceKey)) {
            throw "Set both SPEED_PROBE_TLS_CERT_FILE and SPEED_PROBE_TLS_KEY_FILE"
        }
        if (-not (Test-Path -LiteralPath $SourceCert -PathType Leaf)) {
            throw "TLS certificate not found: $SourceCert"
        }
        if (-not (Test-Path -LiteralPath $SourceKey -PathType Leaf)) {
            throw "TLS private key not found: $SourceKey"
        }
        Copy-Item -LiteralPath $SourceCert -Destination $CertFile -Force
        Copy-Item -LiteralPath $SourceKey -Destination $KeyFile -Force
    }
    elseif ((Test-Path -LiteralPath $CertFile) -and (Test-Path -LiteralPath $KeyFile)) {
        # Reuse the existing development pair so clients can keep trusting it.
    }
    elseif ((Test-Path -LiteralPath $CertFile) -or (Test-Path -LiteralPath $KeyFile)) {
        throw "Incomplete TLS pair in $CertDir; remove it or provide both files"
    }
    elseif (Get-Command openssl -ErrorAction SilentlyContinue) {
        Write-Host "Generating a development self-signed TLS certificate..."
        & openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes `
            -subj "/CN=speed-probe.local" `
            -keyout $KeyFile `
            -out $CertFile 2>$null
        Assert-LastCommand "Failed to generate the development TLS certificate"
    }
    else {
        Write-Warning "OpenSSL is unavailable; TLS development assets were not generated"
        return
    }

    Test-TlsPair
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go 1.21 or newer is required"
}

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
Initialize-TlsPair

$Commit = (& git -C $ProjectRoot rev-parse --short HEAD 2>$null)
if ($LASTEXITCODE -ne 0) { $Commit = "unknown" }
$CompilationTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-s -w -X main.COMMIT=$Commit -X main.COMPILATIONTIME=$CompilationTime"
$OutputFile = Join-Path $DistDir "speed-probe.exe"

Write-Host "Building speed-probe with Mihomo support..."
Push-Location $ProjectRoot
try {
    & go build -trimpath -ldflags $LdFlags -o $OutputFile .
    Assert-LastCommand "Go build failed"
}
finally {
    Pop-Location
}

Write-Host "Built $OutputFile"
if ((Test-Path -LiteralPath $CertFile) -and (Test-Path -LiteralPath $KeyFile)) {
    Write-Host "TLS assets: $CertDir"
}
