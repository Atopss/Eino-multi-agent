@echo off
chcp 65001 >nul 2>nul
setlocal enabledelayedexpansion
title Eino 一键启动

REM ============================================================
REM  Eino 一键启动（纯 bat，自包含）
REM  同时拉起后端(Go) + 前端(Vite)，就绪后自动打开前端地址
REM ============================================================

set "ROOT=%~dp0"
set "BACKEND=%ROOT%eino"
set "FRONTEND=%ROOT%web"
set "BPORT=8899"
set "FPORT=5173"
set "FURL=http://localhost:%FPORT%"
set "BURL=http://localhost:%BPORT%"

echo ========================================
echo            Eino 一键启动
echo ========================================

REM ---------- 1) 校验 .env 并确保 JWT_SECRET 存在 ----------
if not exist "%BACKEND%\.env" (
  echo [错误] 未找到 "%BACKEND%\.env"，无法启动后端。
  echo         可复制 "%BACKEND%\.env.example" 为 .env 后重试。
  pause
  exit /b 1
)
findstr /b /c:"JWT_SECRET=" "%BACKEND%\.env" >nul 2>nul
if errorlevel 1 (
  set "SECRET=%RANDOM%%RANDOM%%RANDOM%%RANDOM%%RANDOM%%RANDOM%%RANDOM%"
  echo.>> "%BACKEND%\.env"
  echo # ---------- 鉴权（首次启动自动生成，请妥善保管） ---------->> "%BACKEND%\.env"
  echo JWT_SECRET=eino-!SECRET!>> "%BACKEND%\.env"
  echo [提示] 已为 .env 自动生成随机 JWT_SECRET。
)

REM ---------- 2) 启动桌面控制守护进程 ----------
set "DAEMON=%BACKEND%\computer"
echo [启动] Computer Daemon (127.0.0.1:9876) ...
start "Eino-Daemon" /min cmd /k "cd /d "%DAEMON%" && echo Eino Computer Daemon && python daemon.py"

REM ---------- 3) 启动后端 ----------
echo [启动] 后端 %BURL% ...
start "Eino-Backend" cmd /k "cd /d "%BACKEND%" && go run . :%BPORT%"

REM ---------- 4) 启动前端（首次自动 npm install） ----------
echo [启动] 前端 %FURL% ...
if exist "%FRONTEND%\node_modules" (
  start "Eino-Frontend" cmd /k "cd /d "%FRONTEND%" && npm run dev"
) else (
  echo [提示] 首次运行，正在为前端安装依赖（npm install）...
  start "Eino-Frontend" cmd /k "cd /d "%FRONTEND%" && npm install && npm run dev"
)

REM ---------- 4) 轮询前端端口，就绪后打开浏览器 ----------
echo [等待] 等待前端就绪（最多 120 秒，首次含依赖安装/编译会更久）...
set /a CNT=0
:WAIT
curl -s -o nul %FURL% >nul 2>nul
if not errorlevel 1 goto READY
set /a CNT+=1
if !CNT! geq 120 goto TIMEOUT
timeout /t 1 /nobreak >nul
goto WAIT

:READY
echo.
echo ========================================
echo   Eino 已启动
echo   前端地址: %FURL%
echo   后端地址: %BURL%
echo   默认账号: admin / admin （请尽快修改）
echo ========================================
start "" %FURL%
goto END

:TIMEOUT
echo.
echo [警告] 前端在 120 秒内仍未就绪，请查看 "Eino-Frontend" 窗口的日志排查。
echo         就绪后可手动访问：%FURL%

:END
echo.
echo 前端 / 后端分别在各自窗口运行，可在对应窗口按 Ctrl+C 停止。
pause
endlocal
