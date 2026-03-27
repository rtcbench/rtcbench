"""SSH command execution on remote machines."""

import logging
import os
import subprocess

log = logging.getLogger("bench")


class SSHRunner:
    """Executes commands on remote machines via SSH."""

    def __init__(self, ssh_key, ssh_user, dry_run=False):
        self.ssh_key = os.path.expanduser(ssh_key)
        self.ssh_user = ssh_user
        self.dry_run = dry_run

    def _ssh_base(self, host):
        return [
            "ssh", "-o", "StrictHostKeyChecking=no",
            "-o", "UserKnownHostsFile=/dev/null",
            "-o", "LogLevel=ERROR",
            "-o", "ConnectTimeout=10",
            "-i", self.ssh_key,
            f"{self.ssh_user}@{host}",
        ]

    def run(self, host, cmd, check=True, capture=True, timeout=60):
        """Run a command on a remote host. Returns CompletedProcess."""
        full_cmd = self._ssh_base(host) + [cmd]
        log.debug("SSH %s: %s", host, cmd)
        if self.dry_run:
            log.info("[dry-run] SSH %s: %s", host, cmd)
            return subprocess.CompletedProcess(full_cmd, 0, stdout="", stderr="")
        return subprocess.run(
            full_cmd,
            capture_output=capture,
            text=True,
            check=check,
            timeout=timeout,
        )

    def run_background(self, host, cmd, log_file="/tmp/bench-bg.log"):
        """Run a command in the background. Returns remote PID string."""
        bg_cmd = f"nohup {cmd} >> {log_file} 2>&1 & echo $!"
        result = self.run(host, bg_cmd, timeout=30)
        pid = result.stdout.strip().split("\n")[-1]
        log.debug("SSH %s: background PID=%s", host, pid)
        return pid

    def kill(self, host, pid):
        """Kill a remote process group by PID, ignore errors."""
        # kill the process group so child processes also die
        self.run(host, f"kill -- -{pid} 2>/dev/null || kill {pid} 2>/dev/null || true",
                 check=False, timeout=15)

    def rsync_from(self, host, remote_path, local_path):
        """rsync files from remote to local."""
        os.makedirs(local_path, exist_ok=True)
        cmd = [
            "rsync", "-az", "--timeout=30",
            "-e", f"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i {self.ssh_key}",
            f"{self.ssh_user}@{host}:{remote_path}/",
            f"{local_path}/",
        ]
        log.debug("rsync from %s:%s -> %s", host, remote_path, local_path)
        if self.dry_run:
            log.info("[dry-run] rsync %s:%s -> %s", host, remote_path, local_path)
            return
        subprocess.run(cmd, check=True, capture_output=True, text=True, timeout=120)

    def rsync_to(self, host, local_path, remote_path, timeout=300):
        """rsync a local file or directory to a remote host."""
        # Append / to source only if it's a directory (sync contents, not dir itself)
        src = f"{local_path}/" if os.path.isdir(local_path) else local_path
        dst = f"{self.ssh_user}@{host}:{remote_path}"
        cmd = [
            "rsync", "-a",
            # Skip metadata that may fail on minimal filesystems (Buildroot rootfs)
            "--no-times", "--no-perms", "--no-owner", "--no-group",
            "--timeout=60",
            "-e", f"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -i {self.ssh_key}",
            src, dst,
        ]
        log.debug("rsync to %s:%s <- %s", host, remote_path, local_path)
        if self.dry_run:
            log.info("[dry-run] rsync to %s:%s <- %s", host, remote_path, local_path)
            return
        subprocess.run(cmd, check=True, capture_output=True, text=True, timeout=timeout)

    def write_remote_file(self, host, remote_path, content):
        """Write content to a file on a remote host."""
        cmd = f"mkdir -p $(dirname {remote_path}) && cat > {remote_path} << 'BENCHEOF'\n{content}\nBENCHEOF"
        self.run(host, cmd, timeout=30)
