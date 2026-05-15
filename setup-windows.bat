@echo off
title Go Audio Broadcaster - Windows Setup
echo === Go Audio Broadcaster - Windows Setup ===
echo.

:: 1. Check for FFmpeg
where ffmpeg >nul 2>nul
if %errorlevel% neq 0 (
    echo [INFO] FFmpeg not found. Attempting to download...
    echo This may take a while (approx 100MB)...
    
    :: Download FFmpeg essentials using curl
    curl -L "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip" -o ffmpeg.zip
    
    if %errorlevel% neq 0 (
        echo [ERROR] Download failed. Please download manually from https://ffmpeg.org/
        pause
        exit /b
    )

    echo [INFO] Extracting FFmpeg...
    :: Use PowerShell to unzip
    powershell -Command "Expand-Archive -Path ffmpeg.zip -DestinationPath . -Force"
    
    :: Move ffmpeg.exe to current folder (finding it in the extracted folder)
    powershell -Command "Get-ChildItem -Recurse -Filter ffmpeg.exe | Copy-Item -Destination ."
    powershell -Command "Get-ChildItem -Recurse -Filter ffprobe.exe | Copy-Item -Destination ."
    
    :: Cleanup
    del ffmpeg.zip
    echo [OK] FFmpeg downloaded and extracted to current folder.
) else (
    echo [OK] FFmpeg already found in system PATH.
)

:: 2. Create output directory
if not exist "output" (
    echo Creating output directory...
    mkdir output
)

:: 3. Identify binary
set BINARY=releases\streamer-windows-amd64.exe
if not exist "%BINARY%" (
    echo [ERROR] Binary not found at %BINARY%
    pause
    exit /b
)

echo.
echo === Setup Complete! ===
echo To start the streamer, run:
echo %BINARY% -port 8080 -config station.cfg
echo.
pause
