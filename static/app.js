document.addEventListener("DOMContentLoaded", () => {
    setupMemoryModal();
    setupRiverAnimation();
    setupPhotoPreview();
});

function setupMemoryModal() {
    const modal = document.getElementById("memoryModal");
    if (!modal) return;

    const photo = document.getElementById("modalPhoto");
    const fallback = document.getElementById("modalPhotoFallback");
    const title = document.getElementById("modalTitle");
    const date = document.getElementById("modalDate");
    const boats = document.querySelectorAll(".memory-boat");

    const closeModal = () => {
        modal.classList.remove("is-open");
        modal.setAttribute("aria-hidden", "true");
        document.body.classList.remove("modal-open");
        photo.removeAttribute("src");
        photo.alt = "";
    };

    boats.forEach((boat) => {
        boat.addEventListener("click", () => {
            title.textContent = boat.dataset.title || "Без названия";
            date.textContent = formatDate(boat.dataset.date || "");
            photo.alt = boat.dataset.title || "Фотография воспоминания";
            fallback.hidden = true;
            photo.hidden = true;
            photo.onload = () => {
                photo.hidden = false;
            };
            photo.onerror = () => {
                photo.hidden = true;
            };
            photo.src = boat.dataset.photo || "";

            modal.classList.add("is-open");
            modal.setAttribute("aria-hidden", "false");
            document.body.classList.add("modal-open");
        });
    });

    modal.querySelectorAll("[data-close-modal]").forEach((element) => {
        element.addEventListener("click", closeModal);
    });

    document.addEventListener("keydown", (event) => {
        if (event.key === "Escape" && modal.classList.contains("is-open")) {
            closeModal();
        }
    });
}

function formatDate(value) {
    if (!value) return "";
    const parts = value.split("-");
    if (parts.length !== 3) return value;

    const date = new Date(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2]));
    return new Intl.DateTimeFormat("ru-RU", {
        day: "numeric",
       month: "long",
        year: "numeric"
    }).format(date);
}

function setupRiverAnimation() {
    const track = document.querySelector(".boat-track");
    const river = document.querySelector(".river-water");
    if (!track || !river) return;

    const boats = track.querySelectorAll(".memory-boat");
    if (boats.length === 0) return;

    // Важный принцип: никаких клонов и CSS-анимации трека.
    // В DOM остаётся ровно один набор кораблей, а движение выполняется
    // одним requestAnimationFrame-циклом.
    track.style.animation = "none";
    track.style.left = "0";
    track.style.transform = "translate3d(0, 0, 0)";

    let x = 0;
    let lastTime = null;
    let viewportWidth = 0;
    let trackWidth = 0;
    let startX = 0;
    let endX = 0;

    // Постоянная скорость, px/сек.
    const speed = 80;

    function measure() {
        viewportWidth = river.clientWidth;
        trackWidth = track.scrollWidth;

        // Старт: весь набор полностью за правым краем.
        startX = viewportWidth;

        // Финиш: весь набор полностью за левым краем.
        endX = -trackWidth;

        // При изменении размера окна начинаем новый цикл с правой стороны.
        x = startX;
        track.style.transform = `translate3d(${x}px, 0, 0)`;
    }

    function frame(timestamp) {
        if (lastTime === null) {
            lastTime = timestamp;
        }

        // Защита от огромного скачка после переключения вкладки/сна ПК.
        const deltaSeconds = Math.min((timestamp - lastTime) / 1000, 0.05);
        lastTime = timestamp;

        x -= speed * deltaSeconds;

        // Как только последний корабль полностью вышел слева,
        // мгновенно возвращаем весь набор за правый край.
        if (x <= endX) {
            x = startX;
        }

        track.style.transform = `translate3d(${x}px, 0, 0)`;
        requestAnimationFrame(frame);
    }

    // После загрузки шрифтов размеры текста могут немного измениться.
    // Поэтому измеряемся ещё раз после первого layout.
    measure();
    requestAnimationFrame(() => {
        measure();
        lastTime = null;
        requestAnimationFrame(frame);
    });

    window.addEventListener("resize", measure, { passive: true });
}

function setupPhotoPreview() {
    const input = document.getElementById("photoInput");
    const preview = document.getElementById("preview");
    const image = document.getElementById("previewImage");
    const name = document.getElementById("previewName");

    if (!input || !preview || !image || !name) return;

    input.addEventListener("change", () => {
        const file = input.files && input.files[0];
        if (!file) {
            preview.hidden = true;
            image.removeAttribute("src");
            name.textContent = "";
            return;
        }

        name.textContent = file.name;
        const objectURL = URL.createObjectURL(file);
        image.src = objectURL;
        preview.hidden = false;

        image.onload = () => URL.revokeObjectURL(objectURL);
    });
}
