using Saki.Core.Services.Audio;
using Saki.Core.Services.MediaIntegration;
using Saki.Core.Services.Player;
using Saki.Core.Services.Subsonic;
using Saki.Terminal.UI;
using Terminal.Gui;

var subsonicService = new SubsonicService();
using var musicPlayerService = new MusicPlayerService(
    new SoundFlowAudioService(),
    subsonicService,
    new WindowsMediaIntegration());

Application.Init();

try
{
    Application.Run(new MainApplication(musicPlayerService, subsonicService));
}
finally
{
    Application.Shutdown();
}
