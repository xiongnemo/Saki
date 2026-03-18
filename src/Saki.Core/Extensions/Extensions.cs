namespace Saki.Core.Extensions;

public static class IntExtensions
{
    public static string ToTimeString(this int seconds)
    {
        var time = TimeSpan.FromSeconds(seconds);
        return time.TotalHours >= 1 
            ? time.ToString(@"h\:mm\:ss") 
            : time.ToString(@"m\:ss");
    }
}

public static class RepeatStatusExtensions
{
    public static Models.RepeatStatus Next(this Models.RepeatStatus current)
    {
        return current switch
        {
            Models.RepeatStatus.None => Models.RepeatStatus.RepeatAll,
            Models.RepeatStatus.RepeatAll => Models.RepeatStatus.RepeatOne,
            Models.RepeatStatus.RepeatOne => Models.RepeatStatus.None,
            _ => Models.RepeatStatus.None
        };
    }
}

public static class ListExtensions
{
    private static readonly Random _random = new();

    public static void Shuffle<T>(this IList<T> list)
    {
        int n = list.Count;
        while (n > 1)
        {
            n--;
            int k = _random.Next(n + 1);
            (list[k], list[n]) = (list[n], list[k]);
        }
    }

    public static T Clamp<T>(this T value, T min, T max) where T : IComparable<T>
    {
        if (value.CompareTo(min) < 0) return min;
        if (value.CompareTo(max) > 0) return max;
        return value;
    }
}