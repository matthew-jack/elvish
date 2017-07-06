package web

const mainPageHTML = `<html>

  <div id="scrollback">
    <div id="progress"></div>
  </div>
  <textarea id="code" rows="4"></textarea>

  <style>
    #code {
      width: 100%;
    }
    #scrollback, #code, #progress {
      font-family: monospace;
      font-size: 11pt;
    }
    .code {
      color: white;
      background-color: black;
    }
    .error, .exception {
      color: red;
    }
    .exception {
      font-weight: bold;
    }
    .server-error {
      color: white;
      background-color: red;
    }
  </style>

  <script>
    // TODO(xiaq): Stream results.
    var $historyIndex = null;
    var $scrollback = document.getElementById('scrollback'),
        $code = document.getElementById('code'),
        $progress = document.getElementById('progress');

    $code.addEventListener('keydown', function(e) {
      if (e.key == 'Enter' &&
          !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        execute();
      } else if (e.key == 'ArrowUp' &&
          !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        var $history = sessionStorage.getItem('history');
        if($history != null) {
          document.getElementById('code').value = getPrevCommand(JSON.parse($history));
        }
      } else if (e.key == 'ArrowDown' &&
          !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        var $history = sessionStorage.getItem('history');
        if($history != null) {
          document.getElementById('code').value = getNextCommand(JSON.parse($history));
        }
      }
    });

    function execute() {
      $historyIndex = null;
      var code = $code.value;
      $code.value = '';
      addToScrollback('code', code);
      addToCommandHistory(code);
      $progress.innerText = 'executing...';

      var req = new XMLHttpRequest();
      req.onloadend = function() {
        $progress.innerText = '';
      };
      req.onload = function() {
        var res = JSON.parse(req.responseText);
        addToScrollback('output', res.OutBytes);
        if (res.OutValues) {
          for (var v of res.OutValues) {
            addToScrollback('output-value', v);
          }
        }
        addToScrollback('error', res.ErrBytes);
        addToScrollback('exception', res.Err);
      };
      req.onerror = function() {
        addToScrollback('server-error', req.responseText
          || req.statusText
          || (req.status == req.UNSENT && "lost connection")
          || "unknown error");
      };
      req.open('POST', '/execute');
      req.send(code);
    }

    function addToScrollback(className, innerText) {
      var $div = document.createElement('div')
      $div.className = className;
      $div.innerText = innerText;
      $scrollback.insertBefore($div, $progress);

      window.scrollTo(0, document.body.scrollHeight);
    }

    function addToCommandHistory(command) {
      var $history = sessionStorage.getItem('history') ? JSON.parse(sessionStorage.getItem('history')) : [];
      if($history.length > 256) $history.shift()
      $history.push(command);
      sessionStorage.setItem('history', JSON.stringify($history));;
    }

    function getPrevCommand($history) {
      if($historyIndex == null) {
        $historyIndex = $history.length - 1;
      } else if($historyIndex <= 0) {
        $historyIndex = 0;
      } else if($historyIndex > 0) {
        $historyIndex -= 1;
      }
      return $history[$historyIndex] != null ? $history[$historyIndex] : "";
    }

    function getNextCommand($history) {
      if($historyIndex == null) {
        return "";
      } else if($historyIndex > $history.length - 1) {
        $historyIndex = null;
        return "";
      }
      $historyIndex += 1;
      return $history[$historyIndex] != null ? $history[$historyIndex] : "";
    }

  </script>

</html>
`
