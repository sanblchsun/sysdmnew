// app/static/agent_modal.js - КАК ДОЛЖНО БЫТЬ:
"use strict";

// Закрытие модалки
window.closeAgentModal = function () {
  const modal = document.getElementById("agent-modal");
  if (modal) {
    modal.style.display = "none";
    modal.innerHTML = "";
  }
};

// Сохранение отдела
async function changeDepartment(agentId) {
  const selected = document.querySelector('input[name="department"]:checked');
  if (!selected) {
    alert("Выберите отдел");
    return;
  }

  const deptId = parseInt(selected.value);
  const btn = document.getElementById("save-department-btn");
  const oldText = btn.textContent;
  btn.textContent = "💾 Сохранение...";
  btn.disabled = true;

  try {
    const response = await fetch(`/api/agent/${agentId}/change-department`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ department_id: deptId }),
    });

    const result = await response.json();

    if (result.status === "success") {
      alert("✅ " + result.message);
      closeAgentModal();
    } else {
      alert("❌ " + result.message);
      btn.textContent = oldText;
      btn.disabled = false;
    }
  } catch (error) {
    alert("Сетевая ошибка");
    console.error(error);
    btn.textContent = oldText;
    btn.disabled = false;
  }
}

// Инициализация
document.addEventListener("DOMContentLoaded", function () {
  // Только привязка кнопок и выбор дефолтного отдела
  const saveBtn = document.getElementById("save-department-btn");
  if (saveBtn) {
    saveBtn.addEventListener("click", function () {
      const agentId = parseInt(this.getAttribute("data-agent-id"));
      changeDepartment(agentId);
    });
  }

  const cancelBtn = document.getElementById("cancel-btn");
  if (cancelBtn) cancelBtn.addEventListener("click", closeAgentModal);

  const closeBtn = document.getElementById("modal-close-btn");
  if (closeBtn) closeBtn.addEventListener("click", closeAgentModal);

  // Выбор текущего отдела
  const currentId = document
    .querySelector(".departments-list")
    ?.getAttribute("data-current-dept-id");
  if (currentId) {
    const radio = document.querySelector(
      `input[name="department"][value="${currentId}"]`,
    );
    if (radio) radio.checked = true;
  }
});
