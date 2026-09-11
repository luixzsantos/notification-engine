@echo off
REM iniciar.bat
REM Sobe o Redis, a API e o Worker automaticamente.
REM So dar 2 cliques neste arquivo no Explorador de Arquivos.

cd /d "%~dp0"

echo ==============================================
echo   Subindo o Redis (Docker)...
echo ==============================================
docker compose up -d

if errorlevel 1 (
    echo.
    echo [ERRO] Nao foi possivel subir o Redis.
    echo Verifique se o Docker Desktop esta aberto e rodando.
    echo.
    pause
    exit /b 1
)

echo.
echo ==============================================
echo   Abrindo a API numa janela nova...
echo ==============================================
start "Notification Engine - API" cmd /k "cd /d "%~dp0" && go run cmd/api/main.go"

timeout /t 2 /nobreak >nul

echo ==============================================
echo   Abrindo o Worker numa janela nova...
echo ==============================================
start "Notification Engine - Worker" cmd /k "cd /d "%~dp0" && go run cmd/worker/main.go"

timeout /t 2 /nobreak >nul

echo.
echo ==============================================
echo   Tudo no ar!
echo   API:   http://localhost:8080
echo   Redis: localhost:6379
echo ==============================================
echo.
echo Duas janelas novas foram abertas (API e Worker).
echo Pode fechar esta janela ou deixar aberta, tanto faz.
echo.
pause
