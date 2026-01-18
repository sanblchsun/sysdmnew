// app/static/agents_table.js

// Подсветка строки таблицы по agent_id
function highlightAgentRow(agentId) {
  const table = document.querySelector(".agents-table");
  if (!table) return;
  table
    .querySelectorAll("tr.is-active")
    .forEach((el) => el.classList.remove("is-active"));
  if (!agentId) return;
  const row = table.querySelector(`tr[data-agent-id="${agentId}"]`);
  if (row) row.classList.add("is-active");
}

// Получаем agent_id из URL
function getAgentIdFromURL() {
  return new URLSearchParams(window.location.search).get("agent_id");
}

// Обновляем URL без перезагрузки
function setAgentIdInURL(agentId) {
  const params = new URLSearchParams(window.location.search);
  if (agentId) params.set("agent_id", agentId);
  else params.delete("agent_id");
  history.replaceState(
    null,
    "",
    `${window.location.pathname}?${params.toString()}`,
  );
}

// Инициализация таблицы
function initAgentTable() {
  const agentId = getAgentIdFromURL();
  highlightAgentRow(agentId);

  const table = document.querySelector(".agents-table");
  if (!table) return;

  table.querySelectorAll("tr").forEach((row) => {
    row.onclick = () => {
      const agentId = row.dataset.agentId;
      highlightAgentRow(agentId);
      setAgentIdInURL(agentId); // URL обновляется только через JS
    };
  });
}

// ---------------- INITIAL LOAD ----------------
window.addEventListener("DOMContentLoaded", initAgentTable);

// ---------------- HTMX SWAP HANDLERS ----------------
document.addEventListener("htmx:afterSwap", (evt) => {
  if (
    evt.target.querySelector(".agents-table") ||
    evt.target.classList.contains("agents-table")
  ) {
    initAgentTable();
  }
});
