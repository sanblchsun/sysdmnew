let statusTimer = null;

function startStatusPolling() {
  stopStatusPolling();

  statusTimer = setInterval(updateStatuses, 5000);
}

function stopStatusPolling() {
  if (statusTimer) {
    clearInterval(statusTimer);
    statusTimer = null;
  }
}

async function updateStatuses() {
  const params = new URLSearchParams(window.location.search);
  if (!params.has("target_id")) return;

  const res = await fetch(`/api/agents/status?${params}`);
  if (!res.ok) return;

  const data = await res.json();

  for (const [id, info] of Object.entries(data.agents)) {
    const row = document.querySelector(`tr[data-agent-id="${id}"]`);
    if (!row) continue;

    const dot = row.querySelector(".online-dot");
    dot.classList.toggle("online", info.online);
    dot.classList.toggle("offline", !info.online);
  }
}

document.body.addEventListener("htmx:afterSwap", (e) => {
  if (e.target.id === "top-right") {
    startStatusPolling();
  }
});
