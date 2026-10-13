package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/savorum/savorum/internal/administration"
	"github.com/spf13/cobra"
)

func newVolumeRecoveryCommand() *cobra.Command {
	var (
		volume, action, encoded, name, source string
		fetchTimeout                          time.Duration
	)

	command := &cobra.Command{Use: "volume-recovery", Short: "Transfer and verify an offline replacement volume", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		if err := actAsVolumeOwner(volume, os.Geteuid(), setVolumeOwner); err != nil {
			return err
		}

		if action == "hold" {
			return holdVolumeRecovery(cmd.Context())
		}

		if len(encoded) > 8<<20 {
			return errors.New("volume recovery request exceeds bounds")
		}

		var (
			raw []byte
			err error
		)
		if encoded == "-" {
			raw, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), (512<<10)+1))
			if err != nil || len(raw) > 512<<10 {
				return errors.New("recovery stdin exceeds bounds")
			}
		} else {
			raw, err = base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return errors.New("invalid recovery request encoding")
			}
		}

		request, err := decodeVolumeRequest(raw)
		if err != nil {
			return err
		}

		if action == "fetch" || source != "" {
			return fetchVolumeFile(cmd, volume, action, request, name, source, fetchTimeout)
		}

		receipt, err := administration.VolumeRecovery(cmd.Context(), volume, action, request, name, 0, nil)
		if err != nil {
			return err
		}

		return json.NewEncoder(cmd.OutOrStdout()).Encode(receipt)
	}}
	command.Flags().StringVar(&volume, "volume", "", "mounted replacement volume")
	command.Flags().StringVar(&action, "action", "status", "hold, status, fetch, restore or verify")
	command.Flags().StringVar(&encoded, "request", "", "base64 immutable manifest or - for bounded JSON stdin")
	command.Flags().StringVar(&name, "file", "", "exact manifest file")
	command.Flags().StringVar(&source, "source", "", "HTTPS location of the exact manifest file for the fetch action")

	command.Flags().DurationVar(&fetchTimeout, "fetch-timeout", 90*time.Second, "maximum transfer fetch duration")
	requireFlags(command, "volume")

	return command
}

// actAsVolumeOwner switches a root process to the owner of the volume root, so
// the serving user can open everything recovery writes. Provider command
// channels can run recovery commands as root.
func actAsVolumeOwner(volume string, euid int, set func(uid, gid int) error) error {
	if euid != 0 {
		return nil
	}

	info, err := os.Stat(volume)
	if err != nil {
		return fmt.Errorf("inspect recovery volume: %w", err)
	}

	uid, gid, ok := fileOwner(info)
	if !ok || uid == 0 {
		return nil
	}

	if err := set(uid, gid); err != nil {
		return fmt.Errorf("act as the recovery volume owner: %w", err)
	}

	return nil
}

// decodeVolumeRequest reads one complete immutable recovery manifest.
func decodeVolumeRequest(raw []byte) (administration.VolumeRequest, error) {
	var request administration.VolumeRequest

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		return request, errors.New("invalid recovery request")
	}

	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return request, errors.New("trailing recovery request")
	}

	return request, nil
}

func fetchVolumeFile(cmd *cobra.Command, volume, action string, request administration.VolumeRequest, name, source string, timeout time.Duration) error {
	if action != "fetch" {
		return errors.New("unexpected transfer source")
	}

	if timeout <= 0 {
		return usageErrorf("fetch-timeout must be positive")
	}

	receipt, err := administration.VolumeFetchWithTimeout(cmd.Context(), volume, request, name, source, timeout)
	if err != nil {
		return err
	}

	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(receipt); err != nil {
		return fmt.Errorf("write fetch receipt: %w", err)
	}

	return nil
}

func holdVolumeRecovery(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
