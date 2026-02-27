(function () {
  "use strict";

  const loginScreen = document.getElementById("login-screen");
  const playerScreen = document.getElementById("player-screen");
  const nothingPlaying = document.getElementById("nothing-playing");
  const nowPlaying = document.getElementById("now-playing");
  const vinyl = document.getElementById("vinyl");
  const albumArt = document.getElementById("album-art");
  const trackNameEl = document.getElementById("track-name");
  const artistNameEl = document.getElementById("artist-name");
  const albumNameEl = document.getElementById("album-name");
  const releaseDateEl = document.getElementById("release-date");
  const elapsedEl = document.getElementById("elapsed");
  const durationEl = document.getElementById("duration");
  const progressFill = document.getElementById("progress-fill");

  let currentTrackId = null;
  let isPlaying = false;
  let progressMs = 0;
  let durationMs = 0;
  let lastPollTime = 0;
  let artMode = "vinyl";

  function formatTime(ms) {
    const totalSec = Math.floor(ms / 1000);
    const min = Math.floor(totalSec / 60);
    const sec = totalSec % 60;
    return min + ":" + String(sec).padStart(2, "0");
  }

  function formatDate(dateStr) {
    if (!dateStr) return "";
    const parts = dateStr.split("-");
    if (parts.length === 3) {
      const d = new Date(dateStr + "T00:00:00");
      return d.toLocaleDateString("en-US", {
        year: "numeric",
        month: "long",
        day: "numeric",
      });
    }
    if (parts.length === 2) {
      const d = new Date(dateStr + "-01T00:00:00");
      return d.toLocaleDateString("en-US", {
        year: "numeric",
        month: "long",
      });
    }
    return dateStr;
  }

  // --- Show/hide views ---
  function showLogin() {
    loginScreen.classList.remove("hidden");
    playerScreen.classList.add("hidden");
  }

  function showPlayer() {
    loginScreen.classList.add("hidden");
    playerScreen.classList.remove("hidden");
  }

  function showNothingPlaying() {
    nothingPlaying.classList.remove("hidden");
    nowPlaying.classList.add("hidden");
    vinyl.classList.remove("playing");
  }

  function showNowPlaying() {
    nothingPlaying.classList.add("hidden");
    nowPlaying.classList.remove("hidden");
  }

  function applyArtMode() {
    if (artMode === "vinyl") {
      vinyl.classList.remove("picture-mode");
      if (isPlaying) {
        vinyl.classList.add("playing");
      } else {
        vinyl.classList.remove("playing");
      }
    } else {
      vinyl.classList.remove("playing");
      vinyl.classList.add("picture-mode");
    }
  }

  function connectConfigStream() {
    const source = new EventSource("/api/config/stream");
    source.onmessage = (e) => {
      try {
        const cfg = JSON.parse(e.data);
        artMode = cfg.art_mode || "vinyl";
        applyArtMode();
      } catch {}
    };
    source.onerror = () => {
      source.close();
      setTimeout(connectConfigStream, 5000);
    };
  }

  async function poll() {
    try {
      const resp = await fetch("/api/now-playing");
      if (resp.status === 401) {
        showLogin();
        return;
      }
      if (!resp.ok) return;

      const data = await resp.json();

      if (!data.item) {
        isPlaying = false;
        showPlayer();
        showNothingPlaying();
        return;
      }

      showPlayer();
      showNowPlaying();

      isPlaying = data.is_playing;
      progressMs = data.progress_ms || 0;
      durationMs = data.item.duration_ms || 0;
      lastPollTime = performance.now();

      applyArtMode();

      const trackId = data.item.id;
      if (trackId !== currentTrackId) {
        currentTrackId = trackId;

        const images = data.item.album?.images;
        if (images && images.length > 0) {
          const img = images.find((i) => i.width === 300) || images[0];
          albumArt.src = img.url;
        }

        trackNameEl.textContent = data.item.name;
        artistNameEl.textContent = (data.item.artists || [])
          .map((a) => a.name)
          .join(", ");
        albumNameEl.textContent = data.item.album?.name || "";
        releaseDateEl.textContent = formatDate(
          data.item.album?.release_date
        );

        durationEl.textContent = formatTime(durationMs);
      }
    } catch {
      // Network error, retry next poll
    }
  }

  function animate() {
    requestAnimationFrame(animate);

    if (isPlaying) {
      const now = performance.now();
      const elapsed = now - lastPollTime;
      const interpolated = Math.min(progressMs + elapsed, durationMs);

      const pct = durationMs > 0 ? (interpolated / durationMs) * 100 : 0;
      progressFill.style.width = pct + "%";
      elapsedEl.textContent = formatTime(interpolated);
    } else {
      const pct = durationMs > 0 ? (progressMs / durationMs) * 100 : 0;
      progressFill.style.width = pct + "%";
      elapsedEl.textContent = formatTime(progressMs);
    }
  }

  const island = document.getElementById("island");
  const islandText = document.getElementById("island-text");

  function showIsland(msg, duration) {
    islandText.textContent = msg;
    island.classList.remove("hidden");
    requestAnimationFrame(() => island.classList.add("show"));
    setTimeout(() => {
      island.classList.remove("show");
      setTimeout(() => island.classList.add("hidden"), 400);
    }, duration || 2000);
  }

  async function syncSession(authenticated) {
    try {
      if (authenticated) {
        const resp = await fetch("/api/session/export");
        if (resp.ok) {
          const data = await resp.json();
          await navigator.clipboard.writeText(JSON.stringify(data));
          showIsland("Session copied to clipboard");
        }
      } else {
        const text = await navigator.clipboard.readText();
        if (!text) return;
        let parsed;
        try { parsed = JSON.parse(text); } catch { return; }
        if (!parsed.refresh_token) return;
        showIsland("Session found, loading...");
        const resp = await fetch("/api/session/import", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: text,
        });
        if (resp.ok) {
          await navigator.clipboard.writeText("");
          location.reload();
        }
      }
    } catch (err) {
      // ign
    }
  }

  async function init() {
    connectConfigStream();
    const resp = await fetch("/api/now-playing");
    const authenticated = resp.status !== 401;

    if (!authenticated) {
      showLogin();
    } else {
      showPlayer();
      if (resp.ok) {
        const data = await resp.json();
        if (data.item) {
          showNowPlaying();
        } else {
          showNothingPlaying();
        }
      }
      poll();
    }

    syncSession(authenticated);
    setInterval(poll, 3000);
    requestAnimationFrame(animate);
  }

  init();
})();
