package encoder

import (
	"encoding/binary"
	"io"
	"sync"
	"time"
)

type MixerChannel struct {
	id     int
	buffer []byte
	mu     sync.Mutex
	active bool
	volume float64
	Muted  bool
}

func (c *MixerChannel) Write(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buffer = append(c.buffer, p...)
	c.active = true
	return len(p), nil
}

func (c *MixerChannel) SetVolume(v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v < 0 { v = 0 }
	if v > 2.0 { v = 2.0 } // Allow up to 200%
	c.volume = v
}

func (c *MixerChannel) SetMute(m bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Muted = m
}

type ChannelStatus struct {
	ID     int     `json:"id"`
	Active bool    `json:"active"`
	Volume float64 `json:"volume"`
	Muted  bool    `json:"muted"`
}

type AudioMixer struct {
	Channels   []*MixerChannel
	Output     io.WriteCloser
	mu         sync.Mutex
	running    bool
	stopChan   chan struct{}
	duckFactor float64
}

func NewAudioMixer(output io.WriteCloser, numChannels int) *AudioMixer {
	m := &AudioMixer{
		Output:     output,
		stopChan:   make(chan struct{}),
		duckFactor: 1.0,
	}
	for i := 0; i < numChannels; i++ {
		m.Channels = append(m.Channels, &MixerChannel{
			id:     i,
			volume: 1.0,
		})
	}
	return m
}

func (m *AudioMixer) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go m.run()
}

func (m *AudioMixer) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopChan)
	m.mu.Unlock()
}

func (m *AudioMixer) run() {
	// 44100Hz, Stereo, S16LE
	// 20ms chunk = 44100 * 2 * 2 * 0.02 = 3528 bytes
	const size = 3528
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.mixAndWrite(size)
		}
	}
}

func (m *AudioMixer) mixAndWrite(size int) {
	mixed := make([]byte, size)
	samples := make([]int32, size/2)

	m.mu.Lock()
	// Auto-ducking logic
	m.Channels[0].mu.Lock()
	announcerActive := m.Channels[0].active && len(m.Channels[0].buffer) >= size && !m.Channels[0].Muted
	m.Channels[0].mu.Unlock()

	target := 1.0
	if announcerActive {
		target = 0.2
	}
	if m.duckFactor > target {
		m.duckFactor -= 0.1
		if m.duckFactor < target { m.duckFactor = target }
	} else if m.duckFactor < target {
		m.duckFactor += 0.05
		if m.duckFactor > target { m.duckFactor = target }
	}
	duck := m.duckFactor
	m.mu.Unlock()

	for i, ch := range m.Channels {
		ch.mu.Lock()
		// Hanya ambil jika ada data utuh (size). Kalau tidak, biarkan dia mengantri.
		// Ini mencegah suara "dicacah".
		if !ch.active || len(ch.buffer) < size || ch.Muted {
			ch.mu.Unlock()
			continue
		}

		vol := ch.volume
		if i > 0 {
			vol = vol * duck
		}

		for j := 0; j < size; j += 2 {
			val := int16(binary.LittleEndian.Uint16(ch.buffer[j : j+2]))
			v := float64(val) * vol
			samples[j/2] += int32(v)
		}

		ch.buffer = ch.buffer[size:]
		if len(ch.buffer) == 0 {
			ch.active = false
		}
		ch.mu.Unlock()
	}

	// Output conversion with clipping protection
	for i := 0; i < len(samples); i++ {
		val := samples[i]
		if val > 32767 { val = 32767 }
		if val < -32768 { val = -32768 }
		binary.LittleEndian.PutUint16(mixed[i*2 : i*2+2], uint16(val))
	}

	m.Output.Write(mixed)
}

func (m *AudioMixer) GetStatus() []ChannelStatus {
	status := make([]ChannelStatus, len(m.Channels))
	for i, ch := range m.Channels {
		ch.mu.Lock()
		status[i] = ChannelStatus{
			ID:     ch.id,
			Active: ch.active,
			Volume: ch.volume,
			Muted:  ch.Muted,
		}
		ch.mu.Unlock()
	}
	return status
}
