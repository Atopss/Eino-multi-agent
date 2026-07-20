# Eino 定时备份脚本（Windows / PowerShell）
# 通过管理 API 触发服务端在线一致性备份（SQLite VACUUM INTO，运行时安全，不锁库）。
# 服务端会自动轮转，仅保留最新 BACKUP_KEEP（默认 30）份。
#
# 用法：
#   .\scripts\backup.ps1
#
# 可配置环境变量（也可直接修改下方默认值）：
#   $env:BACKUP_API   后端地址，默认 http://localhost:8899
#   $env:BACKUP_USER  管理员用户名，默认 admin
#   $env:BACKUP_PASS  管理员密码（不设置则交互输入）
#   $env:BACKUP_INSECURE  设为 1 时跳过 TLS 证书校验（仅自签 HTTPS 调试用）
#
# 计划任务（每日 03:00 自动备份，推荐以 SYSTEM 运行）：
#   $action  = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$PSScriptRoot\backup.ps1`""
#   $trigger = New-ScheduledTaskTrigger -Daily -At "03:00"
#   Register-ScheduledTask -TaskName "EinoBackup" -Action $action -Trigger $trigger -User "SYSTEM" -RunLevel Highest
#
# 或在“任务计划程序”图形界面创建：触发器=每日，操作=启动 powershell.exe，
# 参数： -NoProfile -ExecutionPolicy Bypass -File "<本脚本绝对路径>"

$ErrorActionPreference = "Stop"

$api = if ($env:BACKUP_API) { $env:BACKUP_API } else { "http://localhost:8899" }
$user = if ($env:BACKUP_USER) { $env:BACKUP_USER } else { "admin" }
$insecure = ($env:BACKUP_INSECURE -eq "1")

# 自签证书场景：跳过 TLS 校验（仅调试/内网）
if ($insecure) {
    Add-Type @"
    using System.Net;
    using System.Security.Cryptography.X509Certificates;
    public class TrustAllCertsPolicy : ICertificatePolicy {
        public bool CheckValidationResult(ServicePoint s, X509Certificate c, WebRequest r, int e) { return true; }
    }
"@
    [System.Net.ServicePointManager]::CertificatePolicy = New-Object TrustAllCertsPolicy
}

# 1) 登录获取 token
if (-not $env:BACKUP_PASS) {
    $secure = Read-Host -Prompt "管理员密码 [$user]" -AsSecureString
    $pass = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
} else {
    $pass = $env:BACKUP_PASS
}

$loginBody = @{ username = $user; password = $pass } | ConvertTo-Json
try {
    $login = Invoke-RestMethod -Uri "$api/api/auth/login" -Method Post `
        -ContentType "application/json" -Body $loginBody -TimeoutSec 30
} catch {
    Write-Error "登录失败：$_"; exit 1
}
$token = $login.token
if (-not $token) { Write-Error "登录未返回 token"; exit 1 }

# 2) 触发备份
try {
    $result = Invoke-RestMethod -Uri "$api/api/admin/backup" -Method Post `
        -Headers @{ Authorization = "Bearer $token" } -TimeoutSec 120
} catch {
    Write-Error "备份失败：$_"; exit 1
}

Write-Host "备份完成："
Write-Host "  路径：$($result.path)"
Write-Host "  时间戳：$($result.ts)"
Write-Host "  保留份数：$($result.kept)"
