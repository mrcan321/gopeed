package com.gopeed.gopeed

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import com.gopeed.libgopeed.DownloadFileProvider
import java.io.File
import java.net.URLConnection

/**
 * Native filesystem bridge for the original Gopeed multi-range HTTP engine.
 *
 * The Android platform owns all permissions. An app may read/write files it
 * creates in MediaStore.Downloads without READ_MEDIA, READ/WRITE_EXTERNAL_STORAGE
 * or MANAGE_EXTERNAL_STORAGE. Android 11+ supports direct File/NDK access to
 * app-owned MediaStore objects after they are inserted and opened.
 *
 * No download data is staged in a secondary directory.
 */
class PrivateDownloads(private val context: Context) : DownloadFileProvider {
    private val resolver get() = context.contentResolver
    private val store get() = context.getSharedPreferences("media_store_owned_downloads", Context.MODE_PRIVATE)

    override fun directory(): String =
        Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS).canonicalPath

    @Synchronized override fun prepare(fileName: String?): String {
        if (Build.VERSION.SDK_INT < 30) return ""
        if (fileName.isNullOrBlank() || fileName != File(fileName).name ||
            fileName == "." || fileName == ".." ||
            fileName.any { it.code < 32 || it == '/' || it == '\\' }) return ""
        val row = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, fileName)
            put(MediaStore.MediaColumns.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS + "/")
            put(MediaStore.MediaColumns.MIME_TYPE,
                URLConnection.guessContentTypeFromName(fileName) ?: "application/octet-stream")
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val uri = resolver.insert(
            MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY), row
        ) ?: return ""
        try {
            // Ensure MediaProvider materializes the file before Go's first os.Stat.
            resolver.openFileDescriptor(uri, "rw")?.use {} ?: error("cannot open MediaStore file")
            val absolutePath = resolver.query(
                uri, arrayOf(MediaStore.MediaColumns.DATA), null, null, null
            )?.use { cursor ->
                if (cursor.moveToFirst()) cursor.getString(0) else null
            } ?: error("MediaStore did not return an owned file path")
            val path = File(absolutePath).canonicalPath
            if (File(path).parentFile?.canonicalPath != directory()) {
                error("MediaStore file outside public Download directory")
            }
            // The mapping survives process restarts, so resumed downloads can
            // publish their already created pending file without duplicate rows.
            store.edit().putString(path, uri.toString()).commit()
            return path
        } catch (error: Exception) {
            resolver.delete(uri, null, null)
            return ""
        }
    }

    @Synchronized override fun publish(fullPath: String?): Boolean {
        val path = fullPath ?: return false
        if (File(path).parentFile?.canonicalPath != directory()) return false
        val uriText = store.getString(path, null) ?: return false
        return try {
            val values = ContentValues().apply {
                put(MediaStore.MediaColumns.IS_PENDING, 0)
            }
            resolver.update(Uri.parse(uriText), values, null, null) == 1
        } catch (error: Exception) { false }
    }

    @Synchronized override fun remove(fullPath: String?): Boolean {
        val path = fullPath ?: return false
        if (File(path).parentFile?.canonicalPath != directory()) return false
        val uriText = store.getString(path, null) ?: return false
        return try {
            val deleted = resolver.delete(Uri.parse(uriText), null, null) >= 0
            if (deleted) store.edit().remove(path).apply()
            deleted
        } catch (error: Exception) { false }
    }
}
