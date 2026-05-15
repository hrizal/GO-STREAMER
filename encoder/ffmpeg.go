package encoder

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/streamer/types"
)

const (
	NumSlots   = 30
	SegmentDur = 10.0
)

type AudioEngine struct {
	station   *types.Station
	variants  types.BitrateVariants
	mu        sync.Mutex
	prevFile  string
	tempDir   string
	ffmpegCmd *exec.Cmd
	ffmpegIn  io.WriteCloser
	started   bool
	Mixer       *AudioMixer
	nextCh      int // For alternating playlist channels (1 & 2)
	channelCmds map[int]*exec.Cmd
}

func NewAudioEngine(station *types.Station, variants types.BitrateVariants) *AudioEngine {
	tempDir := filepath.Join(station.OutputDir, ".temp")
	os.MkdirAll(tempDir, 0755)
	return &AudioEngine{
		station:  station,
		variants: variants,
		tempDir:     tempDir,
		nextCh:      1, // Start with channel 1
		channelCmds: make(map[int]*exec.Cmd),
	}
}

type Transition struct {
	PrevFile string
	NextFile string
	IsInsert bool
}

// ─── FFmpeg continuous HLS encoder ───────────────────────────────────

func (ae *AudioEngine) startFFmpeg() error {
	// Root args for reading from stdin pipe
	args := []string{
		"-f", "s16le",
		"-ar", "44100",
		"-ac", "2",
		"-i", "-",
		"-af", "loudnorm=I=-16:TP=-1.5:LRA=11,aresample=44100",
	}

	// For each variant, we add HLS muxer output
	for _, v := range ae.allVariants() {
		os.MkdirAll(v.Dir, 0755)

		hlsFlags := "delete_segments+omit_endlist"
		if v.IsOpus {
			// fMP4 specific flags
			args = append(args,
				"-map", "0:a",
				"-c:a", v.Codec,
				"-b:a", v.Bitrate,
				"-ac", v.Channels, "-ar", v.SampleRate,
				"-f", "hls",
				"-hls_time", strconv.Itoa(ae.station.Config.HlsTime),
				"-hls_list_size", strconv.Itoa(NumSlots),
				"-hls_flags", hlsFlags,
				"-hls_segment_type", "fmp4",
				"-hls_segment_filename", filepath.Join(v.Dir, "seg_%d.mp4"),
				filepath.Join(v.Dir, "index.m3u8"),
			)
		} else {
			// Pure Round Robin using segment muxer
			args = append(args,
				"-map", "0:a",
				"-c:a", v.Codec,
				"-b:a", v.Bitrate,
				"-ac", v.Channels, "-ar", v.SampleRate,
				"-f", "segment",
				"-segment_time", strconv.Itoa(ae.station.Config.HlsTime),
				// Removed -segment_wrap to avoid promotion logic issues
				// Removed -segment_list_flags +live which might cause 'Invalid argument'
				filepath.Join(v.Dir, "raw_seg_%d.ts"),
			)

			// Start a manual playlist manager for this variant
			go ae.manageManualPlaylist(v.Dir)
		}
	}

	cmd := exec.Command("ffmpeg", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	// Capture stderr directly to the main log for unified debugging
	cmd.Stderr = log.Writer()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	ae.ffmpegCmd = cmd
	ae.ffmpegIn = stdin
	ae.started = true

	// Initialize and start Mixer (8 Channels)
	ae.Mixer = NewAudioMixer(stdin, 8)
	go ae.Mixer.Start()

	log.Printf("%s [Encoder] FFmpeg Segmenter started (HLS muxer with Mixer)", ae.station.LogPrefix)
	log.Printf("%s [Encoder] FFmpeg Command: ffmpeg %s", ae.station.LogPrefix, strings.Join(args, " "))

	// Goroutine to monitor FFmpeg exit
	go func() {
		err := cmd.Wait()
		log.Printf("%s [Encoder] FFmpeg Segmenter exited: %v", ae.station.LogPrefix, err)
		ae.mu.Lock()
		ae.started = false
		if ae.Mixer != nil {
			ae.Mixer.Stop()
		}
		ae.mu.Unlock()
	}()

	return nil
}

type variantInfo struct {
	Dir        string
	Codec      string
	Bitrate    string
	Channels   string
	SampleRate string
	Format     string
	Ext        string
	IsOpus     bool
}

func (ae *AudioEngine) allVariants() []variantInfo {
	var variants []variantInfo
	cfg := ae.station.Config

	if cfg.AAC64 {
		variants = append(variants, variantInfo{ae.variants.AAC64, "aac", "64k", "1", "44100", "hls", "ts", false})
	}
	if cfg.AAC96 {
		variants = append(variants, variantInfo{ae.variants.AAC96, "aac", "96k", "2", "44100", "hls", "ts", false})
	}
	if cfg.AAC128 {
		variants = append(variants, variantInfo{ae.variants.AAC128, "aac", "128k", "2", "44100", "hls", "ts", false})
	}
	if cfg.Opus32 {
		variants = append(variants, variantInfo{ae.variants.Opus32, "libopus", "32k", "1", "48000", "hls", "mp4", true})
	}
	if cfg.Opus64 {
		variants = append(variants, variantInfo{ae.variants.Opus64, "libopus", "64k", "2", "48000", "hls", "mp4", true})
	}
	if cfg.Opus96 {
		variants = append(variants, variantInfo{ae.variants.Opus96, "libopus", "96k", "2", "48000", "hls", "mp4", true})
	}

	// Fallback if none enabled (should not happen with default config)
	if len(variants) == 0 {
		variants = append(variants, variantInfo{ae.variants.AAC128, "aac", "128k", "2", "44100", "hls", "ts", false})
	}

	return variants
}

// ─── Audio Feed ───────────────────────────────────────────────────────

func (ae *AudioEngine) feedStream(args []string, channelID int) error {
	ae.mu.Lock()
	started := ae.started
	mixer := ae.Mixer
	ae.mu.Unlock()

	if !started || mixer == nil {
		return fmt.Errorf("AudioMixer not running")
	}

	// Get mixer channel handle
	targetChannel := mixer.Channels[channelID]

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = targetChannel // Feed to mixer channel, not directly to stdin
	log.Printf("%s [Encoder] Feeding stream to Mixer Channel %d...", ae.station.LogPrefix, channelID)
	
	cmd.Stderr = log.Writer()
	
	ae.mu.Lock()
	ae.channelCmds[channelID] = cmd
	ae.mu.Unlock()

	err := cmd.Run()

	ae.mu.Lock()
	if ae.channelCmds[channelID] == cmd {
		delete(ae.channelCmds, channelID)
	}
	ae.mu.Unlock()

	return err
}

func (ae *AudioEngine) StopChannel(channelID int) {
	ae.mu.Lock()
	cmd := ae.channelCmds[channelID]
	ae.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		log.Printf("[Encoder] Stopping Channel %d process...", channelID)
		cmd.Process.Signal(os.Interrupt)
		// Give it a moment to exit naturally, otherwise kill it
		go func() {
			time.Sleep(500 * time.Millisecond)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}()
	}
}

// ─── Execute ──────────────────────────────────────────────────────────

func (ae *AudioEngine) Execute(trans Transition) error {
	ae.mu.Lock()
	if !ae.started {
		if err := ae.startFFmpeg(); err != nil {
			ae.mu.Unlock()
			return fmt.Errorf("start ffmpeg: %w", err)
		}
	}
	ae.mu.Unlock()

	var channelID int
	if trans.IsInsert {
		// Channel 0 for priority (Insert)
		channelID = 0
	} else {
		// Alternate Channels 1 and 2 for Playlist (for crossfading)
		ae.mu.Lock()
		channelID = ae.nextCh
		if ae.nextCh == 1 {
			ae.nextCh = 2
		} else {
			ae.nextCh = 1
		}
		ae.mu.Unlock()
	}

	log.Printf("%s [Encoder] Playing: %s (Channel: %d, insert=%v)", 
		ae.station.LogPrefix, filepath.Base(trans.NextFile), channelID, trans.IsInsert)
	
	args := []string{
		"-re",
		"-i", trans.NextFile,
		"-ac", "2", "-ar", "44100",
		"-f", "s16le", "-acodec", "pcm_s16le", "-",
	}

	// Run feeder (blocking within the caller's goroutine)
	if err := ae.feedStream(args, channelID); err != nil {
		return err
	}

	ae.prevFile = trans.NextFile
	log.Printf("%s [Encoder] Channel %d playback finished", ae.station.LogPrefix, channelID)
	return nil
}

// PlayInstant plays a file directly to a specific channel without waiting in queue
func (ae *AudioEngine) PlayInstant(file string, channelID int) {
	go func() {
		log.Printf("%s [Encoder] PlayInstant: %s (Channel: %d)", 
			ae.station.LogPrefix, filepath.Base(file), channelID)
		
		args := []string{
			"-re",
			"-i", file,
			"-ac", "2", "-ar", "44100",
			"-f", "s16le", "-acodec", "pcm_s16le", "-",
		}
		if err := ae.feedStream(args, channelID); err != nil {
			log.Printf("%s [Encoder] PlayInstant error: %v", ae.station.LogPrefix, err)
		}
	}()
}

func (ae *AudioEngine) manageManualPlaylist(dir string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	durations := make(map[int]float64)
	lastMods := make(map[int]time.Time)

	// Clean up old raw_seg_* and seg_* files to prevent folder bloat and playlist clutter
	if files, err := filepath.Glob(filepath.Join(dir, "raw_seg_*.ts")); err == nil {
		for _, f := range files {
			os.Remove(f)
		}
	}
	if files, err := filepath.Glob(filepath.Join(dir, "seg_*.ts")); err == nil {
		for _, f := range files {
			os.Remove(f)
		}
	}

	// Pre-scan existing files on disk for synchronization
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("seg_%d.ts", i))
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			durations[i] = GetAudioDuration(path)
			lastMods[i] = info.ModTime()
		}
	}

	for {
		ae.mu.Lock()
		started := ae.started
		ae.mu.Unlock()
		if !started {
			return
		}

		// Stage 1: Find all available raw files
		var rawFiles []string
		if matches, err := filepath.Glob(filepath.Join(dir, "raw_seg_*.ts")); err == nil {
			rawFiles = matches
		}

		var maxRawIdx int = -1
		for _, path := range rawFiles {
			base := filepath.Base(path)
			var idx int
			if n, err := fmt.Sscanf(base, "raw_seg_%d.ts", &idx); err == nil && n == 1 {
				if idx > maxRawIdx {
					maxRawIdx = idx
				}
			}
		}

		// Stage 2: Move completed raw files (index < maxRawIdx)
		for _, path := range rawFiles {
			base := filepath.Base(path)
			var idx int
			if n, err := fmt.Sscanf(base, "raw_seg_%d.ts", &idx); err == nil && n == 1 {
				// Files with index smaller than the newest are definitely finished
				if idx < maxRawIdx {
					targetIdx := idx % 10
					targetPath := filepath.Join(dir, fmt.Sprintf("seg_%d.ts", targetIdx))
					
					if info, err := os.Stat(path); err == nil && info.Size() > 0 {
						// Double check: don't promote if it was modified VERY recently (within 500ms)
						// to avoid race condition with FFmpeg closing the file
						if time.Since(info.ModTime()) > 500*time.Millisecond {
							err := os.Rename(path, targetPath)
							if err != nil {
								log.Printf("[HLS] Gagal Rename %s -> %s: %v", base, fmt.Sprintf("seg_%d.ts", targetIdx), err)
							} else {
								log.Printf("[HLS] Promoted %s -> %s", base, fmt.Sprintf("seg_%d.ts", targetIdx))
							}
						}
					}
				}
			} else {
				// If filename is invalid, delete it
				os.Remove(path)
			}
		}
		// Stage 3: Scan confirmed target files
		var newestTargetIdx int = -1
		var maxTargetMod time.Time
		for i := 0; i < 10; i++ {
			path := filepath.Join(dir, fmt.Sprintf("seg_%d.ts", i))
			info, err := os.Stat(path)
			if err == nil && info.Size() > 0 {
				if info.ModTime().After(maxTargetMod) {
					maxTargetMod = info.ModTime()
					newestTargetIdx = i
				}
				// Always update cache if ModTime changes
				if info.ModTime().After(lastMods[i]) {
					durations[i] = GetAudioDuration(path)
					lastMods[i] = info.ModTime()
				}
			}
		}

		// Update: Now we can update playlist even with fewer than 10 files (min 3 for player stability)
		numSegs := len(durations)
		if newestTargetIdx == -1 || numSegs < 3 {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Find the starting sequence (based on FFmpeg's original index to ensure precise increment)
		// Example: If maxRawIdx=78 and there are 10 segments, the first index is 69.
		sequence := maxRawIdx - numSegs + 1
		if sequence < 0 {
			sequence = 0
		}
		firstIdx := (newestTargetIdx - numSegs + 1 + 10) % 10

		var sb strings.Builder
		sb.WriteString("#EXTM3U\n")
		sb.WriteString("#EXT-X-VERSION:3\n")
		sb.WriteString("#EXT-X-ALLOW-CACHE:NO\n")
		sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", ae.station.Config.HlsTime))
		sb.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", sequence))

		for i := 0; i < numSegs; i++ {
			idx := (firstIdx + i) % 10
			dur := durations[idx]
			if dur <= 0 {
				dur = float64(ae.station.Config.HlsTime)
			}
			sb.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", dur))
			sb.WriteString(fmt.Sprintf("seg_%d.ts?t=%d\n", idx, lastMods[idx].Unix()))
		}

		os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(sb.String()), 0644)
		time.Sleep(1 * time.Second)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────

func GetAudioDuration(path string) float64 {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil || len(out) == 0 {
		return 0
	}
	dur, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return dur
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func (ae *AudioEngine) Reset() {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.ffmpegCmd != nil && ae.ffmpegCmd.Process != nil {
		ae.ffmpegCmd.Process.Kill()
	}
	ae.ffmpegCmd = nil
	ae.ffmpegIn = nil
	ae.started = false
	ae.prevFile = ""

	os.RemoveAll(ae.tempDir)
	os.MkdirAll(ae.tempDir, 0755)
}
