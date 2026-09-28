// Bounded HTTPS fallback. Prefer a local MaxMind database for large batches.
function parseAsn(value) {
  var match = String(value || "").match(/^(?:AS)?(\d+)/i);
  return match ? parseInt(match[1],10) : 0;
}
function handler(ip) {
  if (typeof ip !== "string" || !/^[0-9a-fA-F:.]+$/.test(ip)) return {};
  var urls = ["https://ipwho.is/"+encodeURIComponent(ip), "https://ipapi.co/"+encodeURIComponent(ip)+"/json/"];
  for (var i=0; i<urls.length; i++) {
    var response = fetch(urls[i], {timeout:4000, retry:1});
    if (!response || response.statusCode !== 200) continue;
    var data = safeParse(response.body);
    if (!data || data.error || data.success === false || !data.ip || !data.country_code) continue;
    var connection = data.connection || {};
    var org = connection.org || data.org || "";
    return {
      ip:data.ip, country:data.country || data.country_name || "",
      countryCode:data.country_code, country_code:data.country_code,
      city:data.city || "", continentCode:data.continent_code || "",
      organization:org, isp:connection.isp || org,
      asn:parseAsn(connection.asn || data.asn), asnOrg:org, asn_organization:org,
      longitude:Number(data.longitude) || 0, latitude:Number(data.latitude) || 0,
      timezone:typeof data.timezone === "string" ? data.timezone : (data.timezone || {}).id || ""
    };
  }
  // No fake success containing just the input IP: that would poison the six-hour cache.
  return {};
}
