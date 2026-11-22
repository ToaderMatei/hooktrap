; HookTrap — Inno Setup installer script
; Build with:  ISCC.exe /DAppVersion=1.0.0 installer\hooktrap.iss
; (the build.ps1 -Installer switch does this for you)

#ifndef AppVersion
  #define AppVersion "1.0.0"
#endif

#define AppName "HookTrap"
#define AppPublisher "Toader Matei (m68k)"
#define AppURL "https://github.com/ToaderMatei/hooktrap"
#define AppExe "hooktrap.exe"

[Setup]
AppId={{35832390-4173-4A30-9346-455AEC7F6EC3}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
DefaultDirName={autopf}\HookTrap
DefaultGroupName=HookTrap
DisableProgramGroupPage=yes
UninstallDisplayIcon={app}\{#AppExe}
OutputDir=..\dist
OutputBaseFilename=HookTrap-{#AppVersion}-Setup
SetupIconFile=hooktrap.ico
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Install per-user by default: no UAC prompt, and the HKCU PATH / %APPDATA%
; cleanup below then operate on the same user that installed it.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesInstallIn64BitMode=x64compatible
ArchitecturesAllowed=x64compatible
MinVersion=10.0

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "addtopath"; Description: "Add HookTrap to PATH (use 'hooktrap' from any terminal)"; GroupDescription: "Command line:"; Flags: unchecked

[Files]
Source: "..\dist\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion isreadme
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\HookTrap"; Filename: "{app}\{#AppExe}"; Comment: "Start the HookTrap capture server and dashboard"
Name: "{group}\HookTrap on GitHub"; Filename: "{#AppURL}"
Name: "{group}\{cm:UninstallProgram,HookTrap}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\HookTrap"; Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExe}"; Description: "Launch HookTrap now"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{userappdata}\HookTrap"

; ── Add/remove the install dir on the user PATH when the task is selected ──
[Code]
const
  EnvKey = 'Environment';

function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Path: string;
begin
  if (CurStep = ssPostInstall) and WizardIsTaskSelected('addtopath') then
  begin
    if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
      Path := '';
    if NeedsAddPath(ExpandConstant('{app}')) then
    begin
      if (Path <> '') and (Path[Length(Path)] <> ';') then
        Path := Path + ';';
      Path := Path + ExpandConstant('{app}');
      RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
    end;
  end;
end;

procedure RemovePath(Dir: string);
var
  Path: string;
  P: Integer;
begin
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Path) then
    exit;
  P := Pos(Uppercase(Dir), Uppercase(Path));
  if P > 0 then
  begin
    Delete(Path, P, Length(Dir));
    StringChangeEx(Path, ';;', ';', True);
    if (Length(Path) > 0) and (Path[1] = ';') then Delete(Path, 1, 1);
    if (Length(Path) > 0) and (Path[Length(Path)] = ';') then Delete(Path, Length(Path), 1);
    RegWriteStringValue(HKCU, EnvKey, 'Path', Path);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
    RemovePath(ExpandConstant('{app}'));
end;
