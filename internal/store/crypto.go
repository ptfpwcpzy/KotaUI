package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

// state.json 里只有下面这些字段是敏感的，落盘时加密，内存里仍是明文，
// 登录、订阅、流量统计、sing-box 配置生成读的都是内存态，不用改。
//
// state.key 是每台机器随机生成的 32 字节密钥，放数据目录，0600。
// 密钥丢了，state.json 里的敏感字段就解不出来了，备份 state.json 时
// 务必把 state.key 一起备份；密钥文件本身不要进 git、不要外发。

const stateKeyFile = "state.key"

// encPrefix 是密文字段的前缀，也是版本标记：带它的是密文，不带的是旧明文。
const encPrefix = "enc:v1:"

func loadOrCreateKey(dataDir string) ([32]byte, error) {
	var key [32]byte
	path := filepath.Join(dataDir, stateKeyFile)
	if raw, err := os.ReadFile(path); err == nil {
		if len(raw) != 32 {
			return key, errors.New("store: state.key length is not 32 bytes")
		}
		copy(key[:], raw)
		_ = os.Chmod(path, 0600)
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	if _, err := rand.Read(key[:]); err != nil {
		return key, err
	}
	if err := writeFileSync(path, key[:], 0600); err != nil {
		return key, err
	}
	if err := syncDir(dataDir); err != nil {
		return key, err
	}
	return key, nil
}

func encryptValue(key [32]byte, plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, encPrefix) {
		return plain, nil
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptValue(key [32]byte, value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, encPrefix) {
		return value, nil // 空值或旧明文：原样返回，等上层迁移时加密
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("store: ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// hasPlaintext 报告 state 里是否还有未加密的敏感字段。
func hasPlaintext(state config.State) bool {
	for _, inbound := range state.Inbounds {
		for _, v := range []string{inbound.PrivateKey, inbound.ServerPassword, inbound.ObfsPassword} {
			if v != "" && !strings.HasPrefix(v, encPrefix) {
				return true
			}
		}
	}
	for _, client := range state.Clients {
		for _, m := range []map[string]string{client.Credentials, client.TUICPasswords} {
			for _, v := range m {
				if v != "" && !strings.HasPrefix(v, encPrefix) {
					return true
				}
			}
		}
	}
	return state.Settings.Relay.Password != "" && !strings.HasPrefix(state.Settings.Relay.Password, encPrefix)
}

// hasCiphertext 报告 state 里是否已有加密的敏感字段。
func hasCiphertext(state config.State) bool {
	for _, inbound := range state.Inbounds {
		for _, v := range []string{inbound.PrivateKey, inbound.ServerPassword, inbound.ObfsPassword} {
			if strings.HasPrefix(v, encPrefix) {
				return true
			}
		}
	}
	for _, client := range state.Clients {
		for _, m := range []map[string]string{client.Credentials, client.TUICPasswords} {
			for _, v := range m {
				if strings.HasPrefix(v, encPrefix) {
					return true
				}
			}
		}
	}
	return strings.HasPrefix(state.Settings.Relay.Password, encPrefix)
}
func transformState(state config.State, fn func(string) (string, error)) (config.State, error) {
	out := clone(state)
	for i := range out.Inbounds {
		for _, field := range []*string{&out.Inbounds[i].PrivateKey, &out.Inbounds[i].ServerPassword, &out.Inbounds[i].ObfsPassword} {
			v, err := fn(*field)
			if err != nil {
				return config.State{}, err
			}
			*field = v
		}
	}
	for i := range out.Clients {
		for _, m := range []map[string]string{out.Clients[i].Credentials, out.Clients[i].TUICPasswords} {
			for k, v := range m {
				enc, err := fn(v)
				if err != nil {
					return config.State{}, err
				}
				m[k] = enc
			}
		}
	}
	v, err := fn(out.Settings.Relay.Password)
	if err != nil {
		return config.State{}, err
	}
	out.Settings.Relay.Password = v
	return out, nil
}

func encryptState(key [32]byte, state config.State) (config.State, error) {
	return transformState(state, func(v string) (string, error) {
		return encryptValue(key, v)
	})
}

// decryptState 解密一份落盘 state，第二个返回值报告它是否含旧明文字段，
// 第三个报告它是否含密文字段（用来判断密钥是否真的解开过东西）。
func decryptState(key [32]byte, state config.State) (config.State, bool, bool, error) {
	out, err := transformState(state, func(v string) (string, error) {
		return decryptValue(key, v)
	})
	if err != nil {
		return config.State{}, false, false, err
	}
	return out, hasPlaintext(state), hasCiphertext(state), nil
}
