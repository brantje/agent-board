from pathlib import Path
import re
import subprocess


def run(*args: str) -> None:
    subprocess.run(args, check=True)


def mv(src: str, dst: str) -> None:
    source = Path(src)
    target = Path(dst)
    if not source.exists():
        return
    if target.exists():
        raise RuntimeError(f"rename target already exists: {dst}")
    target.parent.mkdir(parents=True, exist_ok=True)
    run("git", "mv", src, dst)


# Stable production filenames.
for src, dst in {
    "apps/server/internal/app/auth_phase2.go": "apps/server/internal/app/auth_management.go",
    "apps/server/internal/app/auth_phase2_password.go": "apps/server/internal/app/password.go",
    "apps/server/internal/app/auth_phase3_groups.go": "apps/server/internal/app/groups.go",
    "apps/server/internal/httpapi/auth_phase2.go": "apps/server/internal/httpapi/auth_management.go",
    "apps/server/internal/httpapi/auth_phase3_groups.go": "apps/server/internal/httpapi/groups.go",
    "apps/server/internal/store/postgres/auth_phase2.go": "apps/server/internal/store/postgres/auth_management.go",
}.items():
    mv(src, dst)

# Rename remaining phase-derived test/support filenames by responsibility.
for path in subprocess.check_output(["git", "ls-files"], text=True).splitlines():
    if path == "apps/server/internal/store/auth_phase2.go":
        continue
    new = path.replace("auth_phase2_", "auth_management_")
    new = new.replace("auth_phase3_groups", "groups")
    new = new.replace("phase5_authorization_hardening", "authorization_hardening")
    new = new.replace("phase5-auth", "authorization")
    if new != path:
        mv(path, new)

# Management persistence is part of the permanent authentication store contract.
auth_store = Path("apps/server/internal/store/auth.go")
text = auth_store.read_text()
management_methods = """
\tCreatePendingUserWithSetupToken(context.Context, User, PasswordToken) (User, PasswordToken, error)
\tListUsers(context.Context) ([]User, error)
\tUpdateUserIdentity(context.Context, string, string, string, string) (User, error)
\tSetUserPasswordIfAuthVersion(context.Context, string, int64, string, bool) (User, error)
\tSetUserDisabled(context.Context, string, bool) (User, error)
\tListUserAuthSessions(context.Context, string, time.Time) ([]AuthSession, error)
\tRevokeAuthSession(context.Context, string, string, time.Time) error
\tRevokeOtherAuthSessions(context.Context, string, string, time.Time) error
\tUpdateAuthSettings(context.Context, AuthSettings) (AuthSettings, error)
"""
if "CreatePendingUserWithSetupToken(" not in text:
    match = re.search(r"(type AuthStore interface \{)(.*?)(\n\})", text, flags=re.S)
    if not match:
        raise RuntimeError("AuthStore interface not found")
    replacement = match.group(1) + match.group(2).rstrip() + management_methods.rstrip("\n") + match.group(3)
    text = text[: match.start()] + replacement + text[match.end() :]
    auth_store.write_text(text)

phase_store = Path("apps/server/internal/store/auth_phase2.go")
if phase_store.exists():
    run("git", "rm", str(phase_store))

# Remove the historical store-extension adapter and call the authoritative store directly.
management = Path("apps/server/internal/app/auth_management.go")
text = management.read_text()
text, removed_helper = re.subn(
    r"\nfunc \(s \*AuthService\) phase2Store\(\) \(store\.AuthPhase2Store, error\) \{.*?\n\}\n",
    "\n",
    text,
    flags=re.S,
)
if removed_helper != 1:
    raise RuntimeError(f"expected one phase2Store helper, removed {removed_helper}")

adapter_blocks = [
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn nil, err\n\t\}\n",
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn PendingUserResult\{\}, err\n\t\}\n",
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn AuthenticatedUser\{\}, err\n\t\}\n",
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn err\n\t\}\n",
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn store\.AuthSettings\{\}, err\n\t\}\n",
]
for pattern in adapter_blocks:
    text = re.sub(pattern, "\n", text)
text = text.replace("extended.", "s.store.")
if "phase2Store" in text or "extended." in text:
    raise RuntimeError("store adapter remained in auth management service")
management.write_text(text)

password = Path("apps/server/internal/app/password.go")
text = password.read_text()
text, removed_adapter = re.subn(
    r"\n\textended, err := s\.phase2Store\(\)\n\tif err != nil \{\n\t\treturn AuthenticatedUser\{\}, err\n\t\}\n",
    "\n",
    text,
)
if removed_adapter != 1:
    raise RuntimeError(f"expected one password store adapter block, removed {removed_adapter}")
password.write_text(text.replace("extended.SetUserPasswordIfAuthVersion", "s.store.SetUserPasswordIfAuthVersion"))

# Delete only assertions that tested the temporary split itself; behavior tests remain.
coverage = Path("apps/server/internal/app/auth_management_coverage_test.go")
text = coverage.read_text()
text = re.sub(
    r"\ntype phase1OnlyAuthStore struct \{\n\tstore\.AuthStore\n\}\n",
    "\n",
    text,
)
text, removed_split_assertions = re.subn(
    r"\n\tphase1Service := phase2ReviewAuthTestService\(t, phase1OnlyAuthStore\{AuthStore: memory\}, &now\)\n\tif _, err := phase1Service\.(?:setPasswordIfAuthVersion|AdminSetDisabled)\([^\n]*\); err == nil \{\n\t\tt\.Fatal\(\"expected phase 2 store requirement failure\"\)\n\t\}\n",
    "\n",
    text,
)
if removed_split_assertions != 2:
    raise RuntimeError(f"expected two obsolete store-split assertions, removed {removed_split_assertions}")
coverage.write_text(text)

# Stable domain/test identifiers.
replacements = {
    "registerAuthPhase2Routes": "registerAuthManagementRoutes",
    "registerAuthPhase3GroupRoutes": "registerGroupRoutes",
    "AuthPhase2Store": "AuthStore",
    "phase2LifecycleMemory": "authLifecycleMemory",
    "phase2MutationErrorStore": "authMutationErrorStore",
    "phase2ReviewAuthTestService": "reviewAuthTestService",
    "phase2Admin": "deploymentAdminActor",
    "phase3GroupHTTPStore": "groupHTTPStore",
    "newPhase3GroupHTTPStore": "newGroupHTTPStore",
    "requirePhase3ErrorCode": "requireGroupErrorCode",
    "TestAuthPhase2Store": "TestAuthStore",
    "TestPhase2": "Test",
    "TestPhase3": "Test",
    "TestPhase4": "Test",
    "TestPhase5": "Test",
}
text_suffixes = {".go", ".ts", ".vue", ".js", ".mjs", ".json", ".yaml", ".yml", ".sql", ".md"}
for root in (Path("apps"), Path("packages")):
    if not root.exists():
        continue
    for file in root.rglob("*"):
        if not file.is_file() or file.suffix not in text_suffixes:
            continue
        old = file.read_text()
        new = old
        for before, after in replacements.items():
            new = new.replace(before, after)
        for number, lower, upper in [
            (1, "auth", "Auth"),
            (2, "authManagement", "AuthManagement"),
            (3, "groups", "Groups"),
            (4, "projectAccess", "ProjectAccess"),
            (5, "authorizationHardening", "AuthorizationHardening"),
        ]:
            new = re.sub(rf"\bphase{number}([A-Z][A-Za-z0-9_]*)", lower + r"\1", new)
            new = re.sub(rf"\bPhase{number}([A-Z][A-Za-z0-9_]*)", upper + r"\1", new)
        for before, after in [
            ("Phase 1", "authentication foundation"),
            ("Phase 2", "authentication management"),
            ("Phase 3", "Groups"),
            ("Phase 4", "Project access"),
            ("Phase 5", "authorization hardening"),
            ("phase 1", "authentication foundation"),
            ("phase 2", "authentication management"),
            ("phase 3", "Groups"),
            ("phase 4", "Project access"),
            ("phase 5", "authorization hardening"),
        ]:
            new = new.replace(before, after)
        if new != old:
            file.write_text(new)

# Temporary maintenance files must not survive the consolidation commit.
run("git", "rm", ".github/consolidate-users-groups.py", ".github/workflows/consolidate-users-groups.yml")
