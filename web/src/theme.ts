import { createTheme, type MantineColorsTuple } from '@mantine/core'

// A teal close to the *arr accent.
const teal: MantineColorsTuple = [
  '#e6fcf9',
  '#d3f5f0',
  '#a8e9df',
  '#7bdccd',
  '#57d1bf',
  '#3fc9b5',
  '#2fc5ae',
  '#1eaa95',
  '#0b9783',
  '#00826f',
]

export const theme = createTheme({
  primaryColor: 'arr',
  colors: { arr: teal },
  primaryShade: { light: 7, dark: 5 },
  defaultRadius: 'sm',
  components: {
    Table: { defaultProps: { fz: 'sm' } },
    TextInput: { defaultProps: { size: 'sm' } },
    PasswordInput: { defaultProps: { size: 'sm' } },
    NumberInput: { defaultProps: { size: 'sm' } },
    Select: { defaultProps: { size: 'sm' } },
    MultiSelect: { defaultProps: { size: 'sm' } },
    Textarea: { defaultProps: { size: 'sm' } },
    Button: { defaultProps: { size: 'xs' } },
    ActionIcon: { defaultProps: { size: 'sm' } },
    Badge: { defaultProps: { size: 'sm' } },
  },
})
