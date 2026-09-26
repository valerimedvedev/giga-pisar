package ru.gigapisar.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder

/**
 * Пока идёт запись диктофона, держит процесс на переднем плане с уведомлением:
 * иначе Android отберёт микрофон, стоит выключить экран или свернуть приложение.
 * Сам звук пишет ViewModel; служба только «держит дверь».
 */
class RecorderService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) { stopSelf(); return START_NOT_STICKY }
        val nm = getSystemService(NotificationManager::class.java)
        nm?.createNotificationChannel(NotificationChannel(CHANNEL, "Диктофон", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val n: Notification = Notification.Builder(this, CHANNEL)
            .setContentTitle("Гига Писарь: идёт запись")
            .setContentText("Диктофон пишет и распознаёт по частям. Откройте, чтобы остановить.")
            .setSmallIcon(android.R.drawable.ic_btn_speak_now)
            .setContentIntent(open)
            .setOngoing(true)
            .build()
        if (Build.VERSION.SDK_INT >= 29) startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE)
        else startForeground(1, n)
        return START_NOT_STICKY
    }

    companion object {
        const val CHANNEL = "recorder"
        const val ACTION_STOP = "stop"
        fun start(ctx: Context) { ctx.startForegroundService(Intent(ctx, RecorderService::class.java)) }
        fun stop(ctx: Context) { ctx.startService(Intent(ctx, RecorderService::class.java).setAction(ACTION_STOP)) }
    }
}
