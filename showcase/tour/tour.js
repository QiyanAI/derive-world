/* SPDX-License-Identifier: Apache-2.0 */
// Static tour behaviour. No network requests, no storage, no analytics.
// The studio is a scripted simulation of the documented create → change →
// branch loop. It creates no object and calls no model.
(function () {
  "use strict";

  var body = document.body;
  body.classList.add("reveal-ready");

  if ("IntersectionObserver" in window) {
    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
      });
    }, { threshold: 0.12, rootMargin: "0px 0px -40px 0px" });
    document.querySelectorAll("[data-reveal]").forEach(function (element) {
      observer.observe(element);
    });
  } else {
    document.querySelectorAll("[data-reveal]").forEach(function (element) {
      element.classList.add("is-visible");
    });
  }

  var dialog = document.getElementById("demo-dialog");
  var openDemo = document.getElementById("open-demo");
  var closeDemo = document.getElementById("close-demo");
  var video = document.getElementById("demo-video");

  if (openDemo && dialog && video) {
    openDemo.addEventListener("click", function () {
      if (typeof dialog.showModal === "function") {
        dialog.showModal();
        video.currentTime = 0;
        video.play().catch(function () {});
      } else {
        window.open("assets/demo-v02.webm", "_blank", "noopener");
      }
    });
  }

  function closeDialog() {
    if (!dialog) return;
    if (video) video.pause();
    if (typeof dialog.close === "function" && dialog.open) dialog.close();
  }

  if (closeDemo) closeDemo.addEventListener("click", closeDialog);
  if (dialog) {
    dialog.addEventListener("click", function (event) {
      if (event.target === dialog) closeDialog();
    });
    dialog.addEventListener("cancel", function (event) {
      event.preventDefault();
      closeDialog();
    });
  }

  var seeds = {
    forest: {
      title: "Star-dust forest",
      premise: "A forest that remembers every light you pick up.",
      image: "assets/world-game.jpg",
      form: "Playable world",
      base: "play"
    },
    library: {
      title: "The Tide Library",
      premise: "A library where the tide decides which book opens.",
      image: "assets/world-story.jpg",
      form: "Living story",
      base: "story"
    },
    workbench: {
      title: "Writing plan",
      premise: "A quiet workbench for a writing plan.",
      image: "assets/world-workspace.jpg",
      form: "Working world",
      base: "work"
    }
  };

  var surfaces = {
    play: { label: "Playable surface", image: "assets/world-game.jpg" },
    story: { label: "Story surface", image: "assets/world-story.jpg" },
    work: { label: "Workbench surface", image: "assets/world-workspace.jpg" },
    motion: { label: "Motion surface", image: "assets/world-motion.jpg" }
  };

  var state = { seed: null, active: new Set(), branched: false };
  var seedChips = Array.prototype.slice.call(document.querySelectorAll("[data-seed]"));
  var surfaceChips = Array.prototype.slice.call(document.querySelectorAll("[data-surface]"));
  var worldImage = document.getElementById("world-image");
  var worldTitle = document.getElementById("world-title");
  var worldPremise = document.getElementById("world-premise");
  var worldBadge = document.getElementById("world-form-badge");
  var surfaceList = document.getElementById("surface-list");
  var approvalStrip = document.getElementById("approval-strip");
  var approvalText = document.getElementById("approval-text");
  var branchPanel = document.getElementById("branch-panel");
  var lineageParent = document.getElementById("lineage-parent");
  var branchButton = document.getElementById("branch-btn");
  var resetButton = document.getElementById("reset-btn");
  var note = document.getElementById("sim-note");

  function setSurfaceChips() {
    surfaceChips.forEach(function (chip) {
      var key = chip.getAttribute("data-surface");
      chip.disabled = !state.seed || state.active.has(key);
      chip.classList.toggle("is-active", state.active.has(key));
    });
  }

  function renderSurfaces() {
    surfaceList.innerHTML = "";
    state.active.forEach(function (key) {
      var meta = surfaces[key];
      if (!meta) return;
      var tag = document.createElement("span");
      tag.className = "tag";
      var thumb = document.createElement("img");
      thumb.src = meta.image;
      thumb.alt = "";
      tag.appendChild(thumb);
      tag.appendChild(document.createTextNode(meta.label));
      surfaceList.appendChild(tag);
    });
  }

  function selectSeed(key) {
    var seed = seeds[key];
    if (!seed) return;
    state.seed = key;
    state.active = new Set([seed.base]);
    state.branched = false;
    seedChips.forEach(function (chip) {
      chip.classList.toggle("is-active", chip.getAttribute("data-seed") === key);
    });
    worldImage.src = seed.image;
    worldImage.alt = "Preview of " + seed.title;
    worldTitle.textContent = seed.title;
    worldPremise.textContent = seed.premise;
    worldBadge.textContent = seed.form;
    lineageParent.textContent = seed.title;
    approvalStrip.hidden = true;
    branchPanel.hidden = true;
    branchButton.disabled = false;
    branchButton.textContent = "Branch this world";
    note.textContent = "Draft created from a scripted prompt. Add a surface or branch it.";
    setSurfaceChips();
    renderSurfaces();
  }

  function addSurface(key) {
    if (!state.seed || state.active.has(key)) return;
    state.active.add(key);
    var meta = surfaces[key];
    renderSurfaces();
    setSurfaceChips();
    approvalText.textContent = "You approved: add a " + meta.label.toLowerCase() +
      ". In the product this is a human confirmation.";
    approvalStrip.hidden = false;
    note.textContent = "Surface added to the same world. Every change is human-approved.";
  }

  function branchWorld() {
    if (!state.seed || state.branched) return;
    state.branched = true;
    branchPanel.hidden = false;
    branchButton.disabled = true;
    branchButton.textContent = "Branched";
    note.textContent = "New branch exists; the original world is intact.";
    if (branchPanel.scrollIntoView) {
      branchPanel.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }
  }

  function resetStudio() {
    state = { seed: null, active: new Set(), branched: false };
    seedChips.forEach(function (chip) { chip.classList.remove("is-active"); });
    worldImage.src = "assets/world-game.jpg";
    worldImage.alt = "Preview of the selected world";
    worldTitle.textContent = "Your world starts here";
    worldPremise.textContent = "Choose one of the three prompts on the left.";
    worldBadge.textContent = "Pick a world";
    surfaceList.innerHTML = "";
    approvalStrip.hidden = true;
    branchPanel.hidden = true;
    branchButton.disabled = true;
    branchButton.textContent = "Branch this world";
    note.textContent = "Pick a world to begin.";
    setSurfaceChips();
  }

  seedChips.forEach(function (chip) {
    chip.addEventListener("click", function () {
      selectSeed(chip.getAttribute("data-seed"));
    });
  });
  surfaceChips.forEach(function (chip) {
    chip.addEventListener("click", function () {
      addSurface(chip.getAttribute("data-surface"));
    });
  });
  branchButton.addEventListener("click", branchWorld);
  resetButton.addEventListener("click", resetStudio);
  setSurfaceChips();
})();
