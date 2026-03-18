using System.Text;

namespace Saki.Terminal.Extensions;

public static class StringExtensions
{
    /// <summary>
    /// Calculate standardized string length accounting for full-width characters
    /// Each CJK character takes 2 columns in monospaced fonts
    /// </summary>
    public static int StandardizedStringLength(this string input)
    {
        int length = 0;
        foreach (var c in input)
        {
            // Wide characters (CJK) take 2 columns, normal characters take 1
            // Use char-based approach for better compatibility
            length += IsWideChar(c) ? 2 : 1;
        }
        return length;
    }
    
    /// <summary>
    /// Check if a character is a wide character (takes 2 columns in display)
    /// </summary>
    private static bool IsWideChar(char c)
    {
        // Basic wide character detection for CJK ranges
        return (c >= 0x1100 && c <= 0x115F) ||   // Hangul Jamo
               (c >= 0x2E80 && c <= 0x2EFF) ||   // CJK Radicals Supplement
               (c >= 0x2F00 && c <= 0x2FDF) ||   // Kangxi Radicals
               (c >= 0x3000 && c <= 0x303F) ||   // CJK Symbols and Punctuation
               (c >= 0x3040 && c <= 0x309F) ||   // Hiragana
               (c >= 0x30A0 && c <= 0x30FF) ||   // Katakana
               (c >= 0x3100 && c <= 0x312F) ||   // Bopomofo
               (c >= 0x3130 && c <= 0x318F) ||   // Hangul Compatibility Jamo
               (c >= 0x3190 && c <= 0x319F) ||   // Kanbun
               (c >= 0x31A0 && c <= 0x31BF) ||   // Bopomofo Extended
               (c >= 0x31C0 && c <= 0x31EF) ||   // CJK Strokes
               (c >= 0x31F0 && c <= 0x31FF) ||   // Katakana Phonetic Extensions
               (c >= 0x3200 && c <= 0x32FF) ||   // Enclosed CJK Letters and Months
               (c >= 0x3300 && c <= 0x33FF) ||   // CJK Compatibility
               (c >= 0x3400 && c <= 0x4DBF) ||   // CJK Unified Ideographs Extension A
               (c >= 0x4E00 && c <= 0x9FFF) ||   // CJK Unified Ideographs
               (c >= 0xA000 && c <= 0xA48F) ||   // Yi Syllables
               (c >= 0xA490 && c <= 0xA4CF) ||   // Yi Radicals
               (c >= 0xAC00 && c <= 0xD7AF) ||   // Hangul Syllables
               (c >= 0xF900 && c <= 0xFAFF) ||   // CJK Compatibility Ideographs
               (c >= 0xFE10 && c <= 0xFE1F) ||   // Vertical Forms
               (c >= 0xFE30 && c <= 0xFE4F) ||   // CJK Compatibility Forms
               (c >= 0xFE50 && c <= 0xFE6F) ||   // Small Form Variants
               (c >= 0xFF00 && c <= 0xFFEF);     // Halfwidth and Fullwidth Forms
    }

    /// <summary>
    /// Pad string to the right, accounting for full-width characters
    /// </summary>
    public static string RunePadRight(this string stringToPad, int totalWidth, char paddingChar = ' ')
    {
        int standardizedStringLength = stringToPad.StandardizedStringLength();
        if (standardizedStringLength >= totalWidth)
        {
            return stringToPad;
        }
        // Adjust padding to account for the difference between display width and string length
        return stringToPad.PadRight(totalWidth - (standardizedStringLength - stringToPad.Length), paddingChar);
    }

    /// <summary>
    /// Pad string to the left, accounting for full-width characters
    /// </summary>
    public static string RunePadLeft(this string stringToPad, int totalWidth, char paddingChar = ' ')
    {
        int standardizedStringLength = stringToPad.StandardizedStringLength();
        if (standardizedStringLength >= totalWidth)
        {
            return stringToPad;
        }
        // Adjust padding to account for the difference between display width and string length
        return stringToPad.PadLeft(totalWidth - (standardizedStringLength - stringToPad.Length), paddingChar);
    }
    
    /// <summary>
    /// Format seconds as MM:SS or HH:MM:SS time string
    /// </summary>
    public static string GetAsMMSS(this int seconds)
    {
        if (seconds < 3600)
        {
            return $"{Math.Floor(seconds / 60d):00}:{seconds % 60:00}";
        }
        else
        {
            return $"{Math.Floor(seconds / 3600d):00}:{Math.Floor((seconds % 3600d) / 60d):00}:{(seconds % 3600d) % 60:00}";
        }
    }
}