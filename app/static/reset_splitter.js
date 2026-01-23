// app/static/reset_splitter.js

(function () {
  "use strict";

  window.resetSplitters = function () {
    console.log("Reset splitters");

    const left = document.getElementById("left-panel");
    const top = document.getElementById("top-right");
    const bottom = document.getElementById("bottom-right");

    if (left) {
      left.style.width = "250px";
      localStorage.setItem("leftPanelWidth", "250px");
    }

    if (top && bottom) {
      top.style.height = "50%";
      bottom.style.height = "50%";
      localStorage.setItem("topPanelHeightPercent", "50");
    }
  };
})();
