@echo off
echo Installing Python dependencies...
pip install -r requirements.txt
echo.
echo Starting Eino Computer Use Daemon on 127.0.0.1:9876...
python daemon.py
pause
