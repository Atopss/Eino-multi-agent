# Eino 恢复脚本（Windows / PowerShell）
# 从 data/backups/<时间戳> 恢复 eino.db / config.json / agents.json / sessions / .env。
#
# 重要：
#   * 恢复前请先【停止 Eino 后端进程】，否则数据库被占用，复制无效甚至损坏！
#   * 恢复会覆盖当前数据，操作不可逆。
#
# 用法：
#   .\scripts\restore.ps1
#   按提示输入备份序号（默认 0 = 最新），输入 YES 确认后开始恢复。

$ErrorActionPreference = "Stop"

# 定位数据目录（脚本位于 eino/scripts，故 ../data 为默认）
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$candidates = @(
    (Join-Path $scriptDir "..\data"),
    (Join-Path $scriptDir "data"),
    (Join-Path (Get-Location) "data"),
    (Join-Path (Get-Location) "eino\data")
)
$dataDir = $null
foreach ($c in $candidates) {
    if (Test-Path $c) { $dataDir = (Resolve-Path $c).Path; break }
}
if (-not $dataDir) { Write-Error "未找到 data 目录（尝试过：$candidates）"; exit 1 }

$backupRoot = Join-Path $dataDir "backups"
if (-not (Test-Path $backupRoot)) { Write-Error "未找到备份目录：$backupRoot"; exit 1 }

$backups = Get-ChildItem $backupRoot -Directory | Sort-Object Name -Descending
if ($backups.Count -eq 0) { Write-Error "没有任何备份"; exit 1 }

Write-Host "可用备份（按时间倒序，[0] 为最新）："
for ($i = 0; $i -lt $backups.Count; $i++) {
    $dbSize = ""
    $dbp = Join-Path $backups[$i].FullName "eino.db"
    if (Test-Path $dbp) { $dbSize = " ($([math]::Round((Get-Item $dbp).Length/1KB, 1)) KB)" }
    Write-Host ("  [{0}] {1}{2}" -f $i, $backups[$i].Name, $dbSize)
}

$idx = Read-Host "请输入要恢复的备份序号（默认 0）"
if ($idx -eq "") { $idx = 0 } else { $idx = [int]$idx }
if ($idx -lt 0 -or $idx -ge $backups.Count) { Write-Error "序号无效"; exit 1 }
$src = $backups[$idx].FullName

Write-Warning "即将把以下备份恢复到 $dataDir ："
Write-Host "  源：$src"
Write-Host "  将覆盖：eino.db / config.json / agents.json / sessions/ / .env"
$confirm = Read-Host "确认恢复？请输入 YES 继续"
if ($confirm -ne "YES") { Write-Host "已取消。"; exit 0 }

function Copy-IfExists($from, $to) {
    if (Test-Path $from) {
        Copy-Item -Path $from -Destination $to -Force
        Write-Host "已恢复：$to"
    }
}

Copy-IfExists (Join-Path $src "eino.db")    (Join-Path $dataDir "eino.db")
Copy-IfExists (Join-Path $src "config.json") (Join-Path $dataDir "config.json")
Copy-IfExists (Join-Path $src "agents.json") (Join-Path $dataDir "agents.json")
Copy-IfExists (Join-Path $src ".env")       (Join-Path $dataDir ".env")

$srcSessions = Join-Path $src "sessions"
if (Test-Path $srcSessions) {
    $dstSessions = Join-Path $dataDir "sessions"
    if (-not (Test-Path $dstSessions)) { New-Item -ItemType Directory -Path $dstSessions | Out-Null }
    Copy-Item -Path (Join-Path $srcSessions "*") -Destination $dstSessions -Recurse -Force
    Write-Host "已恢复：$dstSessions"
}

Write-Host "恢复完成。请重新启动 Eino 后端。"
