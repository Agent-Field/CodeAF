# Running commands on Windows

## Which bash does codeaf use on Windows? (Git Bash)

On Windows, codeaf runs commands in Git for Windows' bash (or another MSYS2-family bash). It looks in this order:

1. `CODEAF_BASH`, an explicit path to a bash.exe. A missing file or a WSL launcher stops the command and names the variable and path.
2. Git found through `git.exe` on PATH, in `cmd`, `bin`, or `mingw64\bin`. If it is a package-manager shim elsewhere, codeaf asks `git --exec-path` (with a three-second timeout) and looks up at most four parent folders for Git Bash. A failed lookup falls through to the next step.
3. Git under `%ProgramW6432%`, `%ProgramFiles%`, `%ProgramFiles(x86)%`, then `%LOCALAPPDATA%\Programs\Git` and `%USERPROFILE%\scoop\apps\git\current`.
4. `bash.exe` on PATH, unless it is a WSL launcher.

For each Git installation, `bin\bash.exe` wins over `usr\bin\bash.exe`. Set `CODEAF_BASH` to choose a Git for Windows or MSYS2 bash.exe yourself; a relative choice is resolved from the current working folder. Unset it to restore this search.

## Can codeaf use WSL to run commands?

codeaf running natively on Windows never uses WSL's bash.exe in Windows system folders or WindowsApps. WSL would run commands in a different system from the files codeaf reads and writes. A command and a file tool could then see different files. Tools installed only inside WSL are not on the native Windows command path; install the Windows version or a tool that works in Git Bash.

Windows commands use Git Bash or another MSYS2-family bash. Setting `CODEAF_BASH` to WSL's launcher stops the command and explains how to choose a usable bash.exe.

## Why do my commands fail on Windows when no bash was found?

codeaf needs Git for Windows' bash or another MSYS2-family bash to run commands on Windows. If none is found, the `bash` shell tool is absent from the model’s tools, no command runs, and the project facts explain:

"codeaf runs commands in Git for Windows' bash, and none was found: install Git for Windows, or set CODEAF_BASH to a bash.exe; WSL's bash.exe is not used."

Install Git for Windows and restart codeaf, or set `CODEAF_BASH` to its bash.exe before starting codeaf. WSL's bash.exe runs in a different system from codeaf's file tools and is never used.

## CODEAF_BASH does not exist or names WSL's launcher

On Windows, `CODEAF_BASH` chooses an explicit bash.exe. An unusable choice stops the command; codeaf does not silently search for another. With `<path>` replaced by the path you set, the missing-file answer is:

`"CODEAF_BASH names <path>, which does not exist. Set it to a Git for Windows or MSYS2 bash.exe, or unset it and codeaf will look for Git Bash."`

The WSL-launcher answer is:

`"CODEAF_BASH names <path>, which is WSL's launcher and runs commands in a different system. Set it to a Git for Windows or MSYS2 bash.exe, or unset it and codeaf will look for Git Bash."`

## How do I write Windows drive paths in Git Bash?

Git Bash uses `/c/…`, `/d/…` for Windows drives, for example `/c/Users/me/project`. codeaf converts paths it places in shell PATH lists and command targets to this form. Relative Windows paths such as `C:skills\bin`, `\tools\bin` and `skills\bin` are first made absolute using Windows' current drive and folder. Already POSIX-shaped paths beginning with `/` stay as written. Values read by native Windows programs keep their Windows form. The worker's project facts name its selected shell and the drive path form. Cygwin's `/cygdrive/` prefix is not supported.

When Git Bash starts a Windows program, it normally converts arguments beginning with `/` into Windows paths. That is how `/c/Users/me` reaches a native Windows tool as a drive path.

## Why does gh api say not found on Windows?

Git Bash normally rewrites arguments beginning with `/` into Windows paths when it starts a Windows program. Thus `gh api /repos/owner/project` can send a Windows path instead of an API route. This is Git Bash's default behavior, also used by commands in codeaf on Windows.

Write the API route without its leading slash: `gh api repos/owner/project`. Or disable conversion for that command: `MSYS_NO_PATHCONV=1 gh api /repos/owner/project`. Keep the default conversion for commands that need Git Bash's `/c/…` drive paths translated for a Windows program.

## Can I run codeaf inside WSL on Windows?

Yes. Run the Linux build of codeaf inside WSL and its commands use Linux bash, beside the Linux files its file tools read and write. The Git for Windows requirement applies to the native Windows build. Native Windows codeaf never selects the WSL launcher as its shell.

## How do I read the rest of a long command output on Windows

When a command prints more than fits, the notice offers an exact `sh` command to read the saved output. It uses Git Bash paths and quotes Windows scratch folders, including folders with spaces or an apostrophe. Run that offered command to read the saved bytes instead of running the original command again.
