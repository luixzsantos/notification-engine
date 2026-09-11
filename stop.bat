@echo off
REM parar.bat
REM Encerra a API, o Worker e derruba o Redis.
REM So dar 2 cliques neste arquivo.

cd /d "%~dp0"

echo ==============================================
echo   Encerrando processo na porta 8080 (API)...
echo ==============================================
for /f "tokens=5" %%p in ('netstat -ano ^| findstr :8080 ^| findstr LISTENING') do (
    taskkill /PID %%p /F >nul 2>&1
)

echo ==============================================
echo   Encerrando processos do Worker (main.exe)...
echo ==============================================
taskkill /IM main.exe /F >nul 2>&1

echo ==============================================
echo   Derrubando o Redis (docker compose down)...
echo ==============================================
docker compose down

echo.
echo ==============================================
echo   Tudo encerrado.
echo ==============================================
echo.
pause
