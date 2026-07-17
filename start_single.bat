@echo off
chcp 65001 >nul 2>nul
setlocal enabledelayedexpansion
title Eino - 前后端一体运行 (后端:8899 + 前端:5173)

set "ROOT=%~dp0"
set "BACKEND=%ROOT%eino"
set "FRONTEND=%ROOT%web"
set "BPORT=8899"
set "FPORT=5173"

echo ========================================
echo       Eino 启动 (单窗口模式)
echo ========================================
echo  后端: http://localhost:%BPORT%
echo  前端: http://localhost:%FPORT%
echo ========================================
echo.

REM ---------- 启动桌面控制守护进程 ----------
set "DAEMON=%BACKEND%\computer"
echo [启动] Computer Daemon (127.0.0.1:9876) ...
start /min cmd /c "cd /d "%DAEMON%" && echo Eino Computer Daemon && python daemon.py"
echo [等待] Daemon 启动中（3秒）...
timeout /t 3 /nobreak >nul

REM ---------- 启动后端（在当前窗口后台运行，输出带【后端】前缀） ----------
echo [启动] 后端编译中...
start /b cmd /c "cd /d "%BACKEND%" && title Eino-Backend && go run . :%BPORT% 2>&1 | powershell -Command "$input | ForEach-Object { Write-Host ('[后端] ' + $_) -ForegroundColor Cyan }""

REM 等待后端编译
echo [等待] 后端编译启动中（约15秒）...
timeout /t 15 /nobreak >nul

REM ---------- 启动前端（当前窗口前台运行）----------
cd /d "%FRONTEND%"
echo.
echo %time% [前端] 启动 Vite 开发服务器...
echo ========================================
echo.
npm run dev

endlocal
