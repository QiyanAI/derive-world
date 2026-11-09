/* SPDX-License-Identifier: Apache-2.0 */
// Static tour behaviour. No network requests, no storage, no analytics.
// The derivation panel is a visual simulation of the documented rule:
// a derive creates an independent child from an immutable source version.
(function () {
  "use strict";

  var lineage = document.getElementById("lineage");
  var note = document.getElementById("sim-note");
  var reset = document.getElementById("sim-reset");
  if (!lineage || !note || !reset) {
    return;
  }

  var children = 0;

  function row(version) {
    var wrapper = document.createElement("div");
    wrapper.className = "lineage-row";

    var source = document.createElement("span");
    source.className = "node source";
    source.textContent = "Parent";
    var sourceDetail = document.createElement("small");
    sourceDetail.textContent = "v" + version;
    source.appendChild(sourceDetail);

    var arrow = document.createElement("span");
    arrow.className = "arrow";
    arrow.setAttribute("aria-hidden", "true");
    arrow.textContent = "→";

    children += 1;
    var child = document.createElement("span");
    child.className = "node child";
    child.textContent = "Child " + children;
    var childDetail = document.createElement("small");
    childDetail.textContent = "independent";
    child.appendChild(childDetail);

    wrapper.appendChild(source);
    wrapper.appendChild(arrow);
    wrapper.appendChild(child);
    return wrapper;
  }

  function derive(version) {
    lineage.appendChild(row(version));
    note.textContent =
      "Derived one child from v" + version +
      ". This page is a simulation: it has no backend, creates no object and calls no model.";
  }

  var buttons = document.querySelectorAll("button[data-version]");
  Array.prototype.forEach.call(buttons, function (button) {
    button.addEventListener("click", function () {
      derive(button.getAttribute("data-version"));
    });
  });

  reset.addEventListener("click", function () {
    children = 0;
    lineage.innerHTML = "";
    var source = document.createElement("div");
    source.className = "node source";
    source.textContent = "Parent world";
    var detail = document.createElement("small");
    detail.textContent = "v1 · v2";
    source.appendChild(detail);
    lineage.appendChild(source);
    note.textContent = "Simulation only. This page has no backend and creates nothing.";
  });
})();
