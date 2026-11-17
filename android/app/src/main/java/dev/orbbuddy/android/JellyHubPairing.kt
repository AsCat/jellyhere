package dev.orbbuddy.android

import android.net.Uri

data class JellyHubPairing(val server: String, val ticket: String) {
    companion object {
        fun parse(value: String, allowHttp: Boolean = false): JellyHubPairing? {
            val uri = runCatching { Uri.parse(value.trim()) }.getOrNull() ?: return null
            if (uri.scheme != "https" && !(allowHttp && uri.scheme == "http")) return null
            if (uri.host.isNullOrBlank() || uri.path != "/pair" || uri.port !in -1..65535 ||
                uri.encodedAuthority.orEmpty().contains('@')) return null
            val ticket = Uri.parse("https://placeholder/?${uri.fragment.orEmpty()}").getQueryParameter("ticket")
                ?: return null
            if (!ticket.matches(Regex("[a-fA-F0-9]{64}"))) return null
            val server = Uri.Builder().scheme(uri.scheme).encodedAuthority(uri.encodedAuthority).build().toString()
            return JellyHubPairing(server, ticket)
        }
    }
}
