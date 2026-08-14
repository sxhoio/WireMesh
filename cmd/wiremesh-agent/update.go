package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wiremesh/wiremesh/internal/wireproto"
)

const (
	agentUpdateServiceName = "wiremesh-agent.service"
	agentUpdateRetries     = 3
)

var errAgentUpdateHandedOff = errors.New("agent update handed off to helper")

// updatePublicKey 由 --update-public-key 提供；配置后更新清单必须携带有效签名，
// 否则拒绝更新（防止仅依赖同信道哈希被 MITM 篡改）。
var updatePublicKey *ecdsa.PublicKey

// parseUpdatePublicKeyPEM 解析 PEM 编码的 ECDSA P-256 公钥。
func parseUpdatePublicKeyPEM(pemText string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("update public key must be an ECDSA key")
	}
	return key, nil
}

// verifyUpdateManifestSignature 用配置的公钥验证清单签名（与服务端
// signUpdateManifest 使用同一规范 JSON 载荷）。
func verifyUpdateManifestSignature(public *ecdsa.PublicKey, manifest agentUpdateManifest) error {
	if strings.TrimSpace(manifest.Signature) == "" {
		return errors.New("update manifest is missing a signature")
	}
	raw, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || len(raw) != 64 {
		return errors.New("update manifest signature is malformed")
	}
	payload, err := json.Marshal(struct {
		Version         string `json:"version"`
		OS              string `json:"os"`
		Arch            string `json:"arch"`
		Size            int64  `json:"size"`
		SHA256          string `json:"sha256"`
		MinAgentVersion string `json:"min_agent_version"`
	}{
		Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch,
		Size: manifest.Size, SHA256: manifest.SHA256, MinAgentVersion: manifest.MinAgentVersion,
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	r := new(big.Int).SetBytes(raw[:32])
	s := new(big.Int).SetBytes(raw[32:])
	if !ecdsa.Verify(public, digest[:], r, s) {
		return errors.New("update manifest signature verification failed")
	}
	return nil
}

type agentUpdateManifest = wireproto.AgentUpdateManifest

func performAgentUpdate(ctx context.Context, client agentClient, statePath, stateDir string, useMTLS bool, commandID string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("agent self-update is only supported on Linux")
	}
	progress := func(message string) {
		log.Printf("agent update: %s", message)
		if err := client.PostCommandProgress(ctx, commandID, message); err != nil {
			log.Printf("agent update progress report failed: %v", err)
		}
	}
	progress("检查更新清单")
	manifest, err := client.PollUpdateManifest(ctx)
	if err != nil {
		return "检查更新清单失败", err
	}
	if !manifest.Available {
		if manifest.Error != "" {
			return "", errors.New(manifest.Error)
		}
		return "", errors.New("server does not have an Agent update package")
	}
	if !manifest.CurrentCompatible {
		return "", fmt.Errorf("current Agent %s is older than the minimum compatible update version %s; please reinstall manually once", agentVersion, manifest.MinAgentVersion)
	}
	if manifest.SHA256 == "" || manifest.Size <= 0 {
		return "", errors.New("server returned an incomplete update manifest")
	}
	// 配置了更新公钥时，清单必须携带有效签名（fail-closed）
	if updatePublicKey != nil {
		if err := verifyUpdateManifestSignature(updatePublicKey, manifest); err != nil {
			return "更新清单签名校验失败", err
		}
	}
	updateDir := filepath.Join(stateDir, "update")
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		return "", fmt.Errorf("create update directory: %w", err)
	}
	stagedPath := filepath.Join(updateDir, "wiremesh-agent-"+safeFilePart(manifest.Version)+"-"+runtime.GOOS+"-"+runtime.GOARCH+".new")
	downloadURL, err := resolveAgentDownloadURL(client.state.Server, manifest.DownloadURL, runtime.GOOS, runtime.GOARCH, manifest.SHA256)
	if err != nil {
		return "", err
	}
	if err := downloadAgentUpdate(ctx, client, downloadURL, stagedPath, manifest, progress); err != nil {
		return "下载或校验更新包失败", err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return "", fmt.Errorf("mark staged update executable: %w", err)
	}
	targetPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve current executable path: %w", err)
	}
	targetPath, _ = filepath.Abs(targetPath)
	progress("下载完成，准备替换并重启 Agent")
	if err := startAgentUpdateHelper(stagedPath, client.state.Server, statePath, stateDir, targetPath, manifest.SHA256, commandID, useMTLS); err != nil {
		return "启动更新助手失败", err
	}
	progress("更新助手已启动，Agent 即将重启")
	return "update helper started", errAgentUpdateHandedOff
}

func resolveAgentDownloadURL(server, raw, osName, arch, sha string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return server + "/agent/download?os=" + osName + "&arch=" + arch + "&sha256=" + sha, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid update download URL: %w", err)
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}
	base, err := url.Parse(strings.TrimRight(server, "/"))
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func downloadAgentUpdate(ctx context.Context, client agentClient, endpoint, stagedPath string, manifest agentUpdateManifest, progress func(string)) error {
	var lastErr error
	for attempt := 1; attempt <= agentUpdateRetries; attempt++ {
		if attempt > 1 {
			progress(fmt.Sprintf("下载失败，正在重试 %d/%d", attempt, agentUpdateRetries))
		} else {
			progress(fmt.Sprintf("下载更新包 %s (%d bytes)", manifest.Version, manifest.Size))
		}
		_ = os.Remove(stagedPath)
		err := func() error {
			request, err := client.newRequest(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return err
			}
			response, err := client.httpClient.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return responseError(response)
			}
			file, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
			if err != nil {
				return err
			}
			defer file.Close()
			hash := sha256.New()
			written, err := copyWithProgress(io.MultiWriter(file, hash), response.Body, manifest.Size, progress)
			if err != nil {
				return err
			}
			if manifest.Size > 0 && written != manifest.Size {
				return fmt.Errorf("downloaded size mismatch: got %d, want %d", written, manifest.Size)
			}
			actual := hex.EncodeToString(hash.Sum(nil))
			if !strings.EqualFold(actual, manifest.SHA256) {
				return fmt.Errorf("update package checksum mismatch: got %s, want %s", actual, manifest.SHA256)
			}
			return nil
		}()
		if err == nil {
			progress("更新包校验通过")
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	_ = os.Remove(stagedPath)
	return lastErr
}

func copyWithProgress(dst io.Writer, src io.Reader, total int64, progress func(string)) (int64, error) {
	buffer := make([]byte, 1024*1024)
	var written int64
	lastReport := time.Now()
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			count, writeErr := dst.Write(buffer[:n])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != n {
				return written, io.ErrShortWrite
			}
			if total > 0 && time.Since(lastReport) >= 2*time.Second {
				progress(fmt.Sprintf("下载中 %.0f%%", float64(written)*100/float64(total)))
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func startAgentUpdateHelper(stagedPath, server, statePath, stateDir, targetPath, sha, commandID string, useMTLS bool) error {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return errors.New("systemd-run is required for safe self-update; reinstall Agent manually on this host")
	}
	unit := "wiremesh-agent-update-" + fmt.Sprint(time.Now().Unix())
	args := []string{
		"--unit", unit,
		"--collect",
		"--property=Type=oneshot",
		"--property=TimeoutStartSec=300",
		stagedPath, "update-helper",
		"--server", server,
		"--state-path", statePath,
		"--state-dir", stateDir,
		"--target", targetPath,
		"--staged", stagedPath,
		"--sha256", sha,
		"--command-id", commandID,
		"--service", agentUpdateServiceName,
		"--mtls=" + fmt.Sprint(useMTLS),
	}
	output, err := exec.Command("systemd-run", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start update helper with systemd-run: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runUpdateHelper(args []string) error {
	flags := flag.NewFlagSet("update-helper", flag.ContinueOnError)
	server := flags.String("server", "", "WireMesh control plane URL")
	statePath := flags.String("state-path", "", "path to identity.json")
	stateDir := flags.String("state-dir", "/var/lib/wiremesh-agent", "agent state directory")
	target := flags.String("target", "/usr/local/bin/wiremesh-agent", "target agent binary")
	staged := flags.String("staged", "", "staged agent binary")
	expectedSHA := flags.String("sha256", "", "expected staged binary checksum")
	commandID := flags.String("command-id", "", "agent command id")
	service := flags.String("service", agentUpdateServiceName, "systemd service name")
	useMTLS := flags.Bool("mtls", false, "use enrolled mTLS certificate")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *statePath == "" {
		*statePath = filepath.Join(*stateDir, "identity.json")
	}
	state, err := loadState(*statePath)
	if err != nil {
		return fmt.Errorf("load agent identity: %w", err)
	}
	if strings.TrimSpace(*server) != "" {
		state.Server = strings.TrimRight(strings.TrimSpace(*server), "/")
	}
	httpClient, err := authenticatedClient(state, *useMTLS)
	if err != nil {
		return fmt.Errorf("configure helper transport: %w", err)
	}
	agentAPI := newAgentClient(httpClient, state)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	progress := func(message string) {
		log.Printf("agent update helper: %s", message)
		if *commandID != "" {
			if err := agentAPI.PostCommandProgress(ctx, *commandID, message); err != nil {
				log.Printf("agent update helper progress report failed: %v", err)
			}
		}
	}
	result, err := runUpdateHelperSteps(ctx, progress, *target, *staged, *expectedSHA, *service)
	if *commandID != "" {
		stateText := "completed"
		if err != nil {
			stateText = "failed"
			if result != "" {
				result += "; "
			}
			result += err.Error()
		}
		if postErr := agentAPI.PostCommandResult(ctx, *commandID, stateText, result); postErr != nil {
			log.Printf("agent update helper result report failed: %v", postErr)
		}
	}
	return err
}

func runUpdateHelperSteps(ctx context.Context, progress func(string), target, staged, expectedSHA, service string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("agent self-update is only supported on Linux")
	}
	target, _ = filepath.Abs(target)
	staged, _ = filepath.Abs(staged)
	progress("校验已下载的新 Agent")
	if expectedSHA != "" {
		actual, err := fileSHA256(staged)
		if err != nil {
			return "", fmt.Errorf("hash staged binary: %w", err)
		}
		if !strings.EqualFold(actual, expectedSHA) {
			return "", fmt.Errorf("staged binary checksum mismatch: got %s, want %s", actual, expectedSHA)
		}
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		return "", fmt.Errorf("chmod staged binary: %w", err)
	}
	progress("替换 Agent 二进制")
	backup, err := replaceAgentBinary(target, staged)
	if err != nil {
		return "", err
	}
	progress("重启 Agent 服务")
	output, err := exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
	if err != nil {
		_ = restoreAgentBinary(target, backup)
		_, _ = exec.CommandContext(ctx, "systemctl", "restart", service).CombinedOutput()
		return "", fmt.Errorf("restart %s: %w: %s", service, err, strings.TrimSpace(string(output)))
	}
	_ = os.Remove(backup)
	_ = os.Remove(staged)
	return "Agent 已更新到 " + agentVersion + " 并重启", nil
}

func replaceAgentBinary(target, staged string) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("stat current agent binary: %w", err)
	}
	directory := filepath.Dir(target)
	temp, err := os.CreateTemp(directory, ".wiremesh-agent-new-*")
	if err != nil {
		return "", fmt.Errorf("stage new agent in target directory: %w", err)
	}
	tempPath := temp.Name()
	if err := copyFileContents(temp, staged); err != nil {
		temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o755
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("chmod new agent binary: %w", err)
	}
	backup := target + ".bak." + fmt.Sprint(time.Now().Unix())
	if err := os.Rename(target, backup); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("backup current agent binary: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		_ = os.Rename(backup, target)
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("activate new agent binary: %w", err)
	}
	return backup, nil
}

func restoreAgentBinary(target, backup string) error {
	if backup == "" {
		return nil
	}
	_ = os.Remove(target)
	return os.Rename(backup, target)
}

func copyFileContents(dst *os.File, source string) error {
	src, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open staged binary: %w", err)
	}
	defer src.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy staged binary: %w", err)
	}
	return nil
}

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func safeFilePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "latest"
	}
	var out strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '_' || char == '-' {
			out.WriteRune(char)
		}
	}
	if out.Len() == 0 {
		return "latest"
	}
	return out.String()
}
