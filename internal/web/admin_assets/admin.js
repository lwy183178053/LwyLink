document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  const message = form.dataset.confirm;
  if (message && !window.confirm(message)) {
    event.preventDefault();
  }
});

document.addEventListener("click", (event) => {
  const target = event.target instanceof Element ? event.target : null;
  if (!target) return;

  const closeButton = target.closest("[data-close-dialog]");
  if (closeButton) {
    const dialog = closeButton.closest("dialog");
    if (dialog instanceof HTMLDialogElement) {
      dialog.close();
    }
    return;
  }

  const trafficButton = target.closest("[data-open-traffic]");
  if (trafficButton instanceof HTMLElement) {
    const dialog = document.getElementById("traffic-dialog");
    const userID = document.getElementById("traffic-user-id");
    const userLabel = document.getElementById("traffic-user-label");
    const userSummary = document.getElementById("traffic-user-summary");
    const amount = document.getElementById("traffic-amount");
    if (dialog instanceof HTMLDialogElement && userID instanceof HTMLInputElement) {
      userID.value = trafficButton.dataset.userId || "";
      const displayName = trafficButton.dataset.displayName || "";
      const username = trafficButton.dataset.username || "";
      if (userLabel) {
        userLabel.textContent = displayName ? `${displayName} · @${username}` : `@${username}`;
      }
      if (userSummary) {
        userSummary.textContent = `已用 ${trafficButton.dataset.used || "-"} / ${trafficButton.dataset.quota || "-"}，剩余 ${trafficButton.dataset.remaining || "-"}，额外余额 ${trafficButton.dataset.extra || "-"}，周期 ${trafficButton.dataset.period || "-"}`;
      }
      if (amount instanceof HTMLInputElement) {
        amount.value = "";
      }
      dialog.showModal();
      window.setTimeout(() => amount instanceof HTMLInputElement && amount.focus(), 0);
    }
    return;
  }

  const button = target.closest("[data-fill-node]");
  if (!(button instanceof HTMLElement)) return;

  const setValue = (id, value) => {
    const input = document.getElementById(id);
    if (input instanceof HTMLInputElement || input instanceof HTMLSelectElement) {
      input.value = value || "";
    }
  };

  setValue("node-id", button.dataset.id);
  setValue("node-name", button.dataset.name);
  setValue("node-overlay", button.dataset.overlay);
  setValue("node-udp", button.dataset.udp);
  setValue("node-health", button.dataset.health);
  setValue("node-max", button.dataset.max || "100");
  setValue("node-key", button.dataset.key);
  setValue("node-enabled", button.dataset.enabled === "true" ? "true" : "false");

  document.getElementById("node-form")?.scrollIntoView({ behavior: "smooth", block: "start" });
});

document.addEventListener("click", (event) => {
  const dialog = event.target;
  if (!(dialog instanceof HTMLDialogElement) || !dialog.classList.contains("traffic-dialog")) return;
  const rect = dialog.getBoundingClientRect();
  const inside =
    event.clientX >= rect.left &&
    event.clientX <= rect.right &&
    event.clientY >= rect.top &&
    event.clientY <= rect.bottom;
  if (!inside) {
    dialog.close();
  }
});
