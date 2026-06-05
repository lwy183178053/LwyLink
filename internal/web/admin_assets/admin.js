document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  const message = form.dataset.confirm;
  if (message && !window.confirm(message)) {
    event.preventDefault();
  }
});

document.addEventListener("click", (event) => {
  const button = event.target instanceof Element ? event.target.closest("[data-fill-node]") : null;
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
  setValue("node-health", button.dataset.health);
  setValue("node-max", button.dataset.max || "100");
  setValue("node-key", button.dataset.key);
  setValue("node-enabled", button.dataset.enabled === "true" ? "true" : "false");

  document.getElementById("node-form")?.scrollIntoView({ behavior: "smooth", block: "start" });
});
