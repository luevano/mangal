package oauth

// should only be used on implicit grant due to
// access token being part of the fragment
const resendFragmentAsParam = `
<html>
<head>
</head>
<body onload="resend()">

<script>
  // the token stays out of the browser history: the fragment is dropped from this entry
  // and sent with a request, not a navigation
  function resend(){
    var hash = window.location.hash.substring(1);
    history.replaceState(null, '', window.location.pathname);
    fetch('?' + hash)
      .then(function (r) { return r.text(); })
      .then(function (t) { document.body.textContent = t; })
      .catch(function () { window.location.replace('?' + hash); });
  }
</script>

</body>
</html>
`
