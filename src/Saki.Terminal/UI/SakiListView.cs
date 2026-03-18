using Saki.Terminal.Extensions;
using System.Collections.ObjectModel;
using Terminal.Gui;

namespace Saki.Terminal.UI;

/// <summary>
/// Custom ListView for Saki that properly handles full-width/half-width character spacing
/// and type-ahead search. Based on the original SonicLairListView implementation.
/// </summary>
public class SakiListView<T> : ListView
{
    private readonly Func<T, string> _serializer;
    private List<T>? _items;

    public List<T>? Items => _items;

    public event EventHandler<T>? ItemSelected;

    // Type-ahead search state
    private DateTime _lastKeyPressTime = DateTime.Now;
    private string _searchTerm = "";

    private readonly Dictionary<Key, Action> _listHotkeys = new();

    private static readonly HashSet<KeyCode> _movementKeyCodes = new()
    {
        KeyCode.CursorDown, KeyCode.CursorUp, KeyCode.CursorLeft, KeyCode.CursorRight,
        KeyCode.Home, KeyCode.End, KeyCode.PageUp, KeyCode.PageDown
    };

    private Action<SakiListView<T>>? _onLeave = null;

    public SakiListView(Func<T, string> serializer)
    {
        _serializer = serializer;
        CanFocus = true;
    }

    public void SetOnLeave(Action<SakiListView<T>> action)
    {
        _onLeave = action;
    }

    public void RegisterHotKey(Key key, Action action)
    {
        _listHotkeys[key] = action;
    }

    public void FocusFirst()
    {
        SetFocus();
        if (_items != null && _items.Count > 0)
        {
            SelectedItem = 0;
            EnsureSelectedItemVisible();
        }
    }

    /// <summary>
    /// Handle Enter for item selection, per-list hotkeys, and type-ahead search.
    /// Arrow keys, Page Up/Down, Home/End are all handled by the base ListView.
    /// </summary>
    protected override bool OnKeyDown(Key keyEvent)
    {
        // Per-list Ctrl hotkeys (e.g. Ctrl+M to add to playlist)
        if (_listHotkeys.ContainsKey(keyEvent))
        {
            _listHotkeys[keyEvent]();
            return true;
        }

        // Enter to select current item
        if (keyEvent.KeyCode == KeyCode.Enter)
        {
            var selectedItem = GetSelectedItem();
            if (selectedItem != null)
            {
                ItemSelected?.Invoke(this, selectedItem);
                return true;
            }
        }

        // Let Backspace pass through to the parent for back-navigation
        if (keyEvent.KeyCode == KeyCode.Backspace)
            return false;

        // Ctrl/Alt combos not in our hotkey dictionary: let them bubble to MainApplication
        if (keyEvent.IsCtrl || keyEvent.IsAlt)
            return false;

        // Let the base ListView handle all movement keys (arrows, page, home/end)
        if (_movementKeyCodes.Contains(keyEvent.KeyCode))
            return base.OnKeyDown(keyEvent);

        // Type-ahead search: printable characters jump to matching item
        if (!keyEvent.IsShift && keyEvent.KeyCode != KeyCode.Enter && keyEvent.KeyCode != KeyCode.Tab)
        {
            var keyChar = (char)keyEvent;
            if (char.IsLetterOrDigit(keyChar) || keyChar == ' ')
            {
                var elapsed = (DateTime.Now - _lastKeyPressTime).TotalMilliseconds;
                if (elapsed > 500)
                    _searchTerm = "";

                _lastKeyPressTime = DateTime.Now;
                _searchTerm += keyChar;

                if (_items != null)
                {
                    var match = _items.FirstOrDefault(item =>
                        item?.ToString()?.StartsWith(_searchTerm, StringComparison.OrdinalIgnoreCase) ?? false);
                    if (match != null)
                    {
                        var index = _items.IndexOf(match);
                        SelectedItem = index;
                        EnsureSelectedItemVisible();
                    }
                }
                return true;
            }
        }

        return base.OnKeyDown(keyEvent);
    }

    public void SetDataSource(List<T> items)
    {
        _items = items;

        if (items == null || items.Count == 0)
        {
            SetSource(new ObservableCollection<string>());
            return;
        }

        var maxDisplayWidth = items.Max(item => _serializer(item).StandardizedStringLength());

        var formattedItems = items.Select(item =>
        {
            var text = _serializer(item);
            return text.RunePadRight(maxDisplayWidth);
        }).ToList();

        SetSource(new ObservableCollection<string>(formattedItems));

        if (items.Count > 0)
        {
            SelectedItem = 0;
            EnsureSelectedItemVisible();
        }
    }

    public T? GetSelectedItem()
    {
        if (_items == null || SelectedItem < 0 || SelectedItem >= _items.Count)
            return default(T);

        return _items[SelectedItem];
    }

    public void ScrollToItem(int index)
    {
        if (_items == null || index < 0 || index >= _items.Count)
            return;

        SelectedItem = index;
        EnsureSelectedItemVisible();
    }

    public bool SelectItem(Func<T, bool> predicate)
    {
        if (_items == null)
            return false;

        var item = _items.FirstOrDefault(predicate);
        if (item != null)
        {
            var index = _items.IndexOf(item);
            ScrollToItem(index);
            return true;
        }
        return false;
    }
}
