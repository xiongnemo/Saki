#include <mutex>
#include <queue>

#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Media.h>
#include <winrt/Windows.Media.Playback.h>
#include <winrt/Windows.Storage.h>
#include <winrt/Windows.Storage.Streams.h>

using namespace winrt;
using namespace Windows::Media;
using namespace Windows::Media::Playback;
using namespace Windows::Storage;
using namespace Windows::Storage::Streams;

namespace
{
    std::mutex g_mutex;
    std::queue<int> g_commands;
    MediaPlayer g_player{ nullptr };
    SystemMediaTransportControls g_smtc{ nullptr };
    winrt::event_token g_button_token{};
    bool g_initialized = false;

    constexpr int command_none = 0;
    constexpr int command_play = 1;
    constexpr int command_pause = 2;
    constexpr int command_stop = 3;
    constexpr int command_next = 4;
    constexpr int command_previous = 5;

    void push_command(int command)
    {
        std::lock_guard lock(g_mutex);
        g_commands.push(command);
    }

    void on_button_pressed(
        SystemMediaTransportControls const&,
        SystemMediaTransportControlsButtonPressedEventArgs const& args)
    {
        switch (args.Button())
        {
        case SystemMediaTransportControlsButton::Play:
            push_command(command_play);
            break;
        case SystemMediaTransportControlsButton::Pause:
            push_command(command_pause);
            break;
        case SystemMediaTransportControlsButton::Stop:
            push_command(command_stop);
            break;
        case SystemMediaTransportControlsButton::Next:
            push_command(command_next);
            break;
        case SystemMediaTransportControlsButton::Previous:
            push_command(command_previous);
            break;
        default:
            break;
        }
    }
}

extern "C" __declspec(dllexport) int saki_smtc_init()
{
    try
    {
        if (!g_initialized)
        {
            winrt::init_apartment(apartment_type::multi_threaded);
            g_initialized = true;
        }

        g_player = MediaPlayer();
        g_player.CommandManager().IsEnabled(false);
        g_smtc = g_player.SystemMediaTransportControls();
        g_smtc.IsEnabled(false);
        g_smtc.IsPlayEnabled(true);
        g_smtc.IsPauseEnabled(true);
        g_smtc.IsStopEnabled(true);
        g_smtc.IsNextEnabled(true);
        g_smtc.IsPreviousEnabled(true);
        g_smtc.PlaybackStatus(MediaPlaybackStatus::Closed);
        g_button_token = g_smtc.ButtonPressed(&on_button_pressed);
        return 0;
    }
    catch (...)
    {
        return 1;
    }
}

extern "C" __declspec(dllexport) int saki_smtc_update_now_playing(
    wchar_t const* title,
    wchar_t const* artist,
    wchar_t const* album,
    wchar_t const* thumbnail_path)
{
    try
    {
        if (!g_smtc)
        {
            return 2;
        }

        g_smtc.IsEnabled(true);
        auto updater = g_smtc.DisplayUpdater();
        updater.ClearAll();
        updater.Type(MediaPlaybackType::Music);
        auto props = updater.MusicProperties();
        props.Title(title ? title : L"");
        props.Artist(artist ? artist : L"");
        props.AlbumTitle(album ? album : L"");

        if (thumbnail_path != nullptr && thumbnail_path[0] != L'\0')
        {
            try
            {
                auto file = StorageFile::GetFileFromPathAsync(thumbnail_path).get();
                updater.Thumbnail(RandomAccessStreamReference::CreateFromFile(file));
            }
            catch (...)
            {
            }
        }

        updater.Update();
        return 0;
    }
    catch (...)
    {
        return 1;
    }
}

extern "C" __declspec(dllexport) int saki_smtc_set_playback_state(int state)
{
    try
    {
        if (!g_smtc)
        {
            return 2;
        }

        switch (state)
        {
        case 1:
            g_smtc.PlaybackStatus(MediaPlaybackStatus::Playing);
            break;
        case 2:
            g_smtc.PlaybackStatus(MediaPlaybackStatus::Paused);
            break;
        default:
            g_smtc.PlaybackStatus(MediaPlaybackStatus::Stopped);
            break;
        }
        return 0;
    }
    catch (...)
    {
        return 1;
    }
}

extern "C" __declspec(dllexport) int saki_smtc_poll_command()
{
    std::lock_guard lock(g_mutex);
    if (g_commands.empty())
    {
        return command_none;
    }
    int command = g_commands.front();
    g_commands.pop();
    return command;
}

extern "C" __declspec(dllexport) void saki_smtc_close()
{
    try
    {
        if (g_smtc)
        {
            g_smtc.ButtonPressed(g_button_token);
            g_smtc.IsEnabled(false);
            g_smtc = nullptr;
        }
        g_player = nullptr;
        if (g_initialized)
        {
            winrt::uninit_apartment();
            g_initialized = false;
        }
    }
    catch (...)
    {
    }
}
