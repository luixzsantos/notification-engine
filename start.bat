@echo off
REM iniciar.bat
REM Sobe Redis, API, Worker e abre a interface web.

cd /d "%~dp0"

echo ==============================================
echo   Subindo Redis + PostgreSQL + Prometheus (Docker)...
echo ==============================================
docker compose up -d

if errorlevel 1 (
    echo.
    echo [ERRO] Nao foi possivel subir a infraestrutura.
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

echo ==============================================
echo   Abrindo a interface web...
echo ==============================================

start "" "%~dp0main.html"

echo.
echo ==============================================
echo   Tudo no ar!
echo   API:        http://localhost:8080
echo   Metricas:   http://localhost:8080/metrics (API) e :9091/metrics (worker)
echo   Prometheus: http://localhost:9090
echo   Interface:  main.html (enviar + dashboard + links)
echo ==============================================
echo.
echo Duas janelas novas foram abertas:
echo API e Worker.
echo.
pause
