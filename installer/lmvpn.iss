; LMVPN Windows Installer — Inno Setup script
;
; Build:
;   ISCC /DAppVersion=0.3.6-abc123 installer/lmvpn.iss
;   wine "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" /DAppVersion=0.3.6-abc123 installer/lmvpn.iss
;
; The exes (lmvpn.exe, lmvpnd.exe) must exist in ../build/ before compiling.
; Run `make build-windows` first (or `make installer-windows` which does both).

#ifndef AppVersion
  #define AppVersion "dev"
#endif

#define AppName       "LMVPN"
#define AppPublisher  "LMVPN"
#define AppExeName    "lmvpn.exe"
#define DaemonExeName "lmvpnd.exe"
#define AppURL        "https://github.com/lmvpn/lmvpn_client"

[Setup]
AppId={{8F7B3A2E-1D4C-4B5E-9F6A-2E3C7D8E1F0A}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
OutputDir=..\build
OutputBaseFilename=LMVPN-Setup-{#AppVersion}
SetupIconFile=..\resources\icon.ico
UninstallDisplayIcon={app}\{#AppExeName}
UninstallDisplayName={#AppName}

[Languages]
Name: "chinesesimp"; MessagesFile: "ChineseSimplified.isl"
Name: "english";    MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式(&D)"; GroupDescription: "附加选项:"; Flags: unchecked

[Files]
Source: "..\build\{#AppExeName}";     DestDir: "{app}"; Flags: ignoreversion
Source: "..\build\{#DaemonExeName}";  DestDir: "{app}"; Flags: ignoreversion
Source: "..\resources\wintun.dll";    DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}";           Filename: "{app}\{#AppExeName}"
Name: "{group}\卸载 {#AppName}";       Filename: "{uninstallexe}"
Name: "{commondesktop}\{#AppName}";   Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "立即启动 {#AppName}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: files; Name: "{app}\wintun.dll"

[Code]

// Kill running LMVPN processes (GUI + daemon) by image name.
// Returns True if at least one process was found and killed.
function KillProcess(const ExeName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  // taskkill /IM <name> /F /T — force-kill process tree
  if Exec(ExpandConstant('{cmd}'), '/C taskkill /IM ' + ExeName + ' /F /T',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    // exit code 0 = killed, 128 = not found — both are fine
    Result := (ResultCode = 0);
  end;
end;

// PrepareToInstall runs before file extraction. We kill any running
// LMVPN processes here so that the exes can be overwritten cleanly.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  KillProcess('{#DaemonExeName}');
  KillProcess('{#AppExeName}');
  // Give the OS a moment to release file handles
  Sleep(800);
  Result := '';
end;

// Kill processes before uninstall as well.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    KillProcess('{#DaemonExeName}');
    KillProcess('{#AppExeName}');
    Sleep(500);
  end;
end;

// Check if a process with the given image name is currently running.
function IsProcessRunning(const ExeName: String): Boolean;
var
  ResultCode: Integer;
begin
  Result := False;
  // tasklist exits 0 if the process is found, 1 if not found
  if Exec(ExpandConstant('{cmd}'), '/C tasklist /FI "IMAGENAME eq ' + ExeName + '" /NH /FO CSV | findstr /I "' + ExeName + '"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
    Result := (ResultCode = 0);
end;

// Warn the user if LMVPN is still running and offer to kill it
// before proceeding from the directory selection page.
function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = wpSelectDir then
  begin
    if IsProcessRunning('{#AppExeName}') or IsProcessRunning('{#DaemonExeName}') then
    begin
      if MsgBox('{#AppName} 正在运行中，需要先关闭才能继续安装。' + #13#10 + #13#10 + '是否立即关闭？',
                mbConfirmation, MB_YESNO) = IDYES then
      begin
        KillProcess('{#DaemonExeName}');
        KillProcess('{#AppExeName}');
        Sleep(800);
        // Verify they're actually dead
        if IsProcessRunning('{#AppExeName}') or IsProcessRunning('{#DaemonExeName}') then
        begin
          MsgBox('无法关闭 {#AppName} 进程，请手动结束任务后重试。', mbError, MB_OK);
          Result := False;
        end;
      end
      else
        Result := False;
    end;
  end;
end;
