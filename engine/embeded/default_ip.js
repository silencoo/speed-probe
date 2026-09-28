// Separate IPv4 and IPv6 endpoints: repeated IPv4 providers cannot detect dual stack.
function ip_resolve_default() {
  var urls = ["https://api.ipify.org?format=json", "https://api6.ipify.org?format=json"];
  var ips = [];
  for (var i=0; i<urls.length; i++) {
    var response = fetch(urls[i], {timeout:4000,retry:1});
    if (!response || response.statusCode !== 200) continue;
    var data = safeParse(response.body);
    if (data && typeof data.ip === "string" && /^[0-9a-fA-F:.]+$/.test(data.ip) && ips.indexOf(data.ip)<0) ips.push(data.ip);
  }
  return ips; // Go validates each address with net.ParseIP.
}
